#property strict
#property version "1.108"
#property description "Demo-only executor for Go M3 signals. Attach once per demo account."
#include <Trade/Trade.mqh>
#include "ExecutionPolicy.mqh"

input string BridgeURL="http://127.0.0.1:8080";
input string BridgeToken="";
input string DemoServer="";
input long DemoLogin=0; // Explicitly pin to the demo account shown in MT5.
input bool EnableEntries=false;
input double CommissionPerLot=0; // Estimated round-trip USD/lot.
input double SlippageAllowance=0.05; // Price units, included in risk estimate.
input string TradeSymbol="XAUUSDm";
input double MinPairRewardRisk=1.0; // Combined TP1 + TP2 net reward / full two-leg stop loss.
double MinTargetDistance=5.0,ProfitLockDistance=2.0;
const double Lots=0.06, PairRisk=200, DailyLoss=1000;
ulong Magic1=2601001, Magic2=2601002;
CTrade trade;
string prefix,accountPrefix;
string APIPath() {
 if(TradeSymbol=="EURUSDm") return "/api/pairs/EURUSDm";
 if(TradeSymbol=="USDJPYm") return "/api/pairs/USDJPYm";
 return "/api";
}
int lockfile=INVALID_HANDLE;
datetime startupBar=0;
double intentTP1=0,intentTP2=0;
string rejection="";
bool storageOK=true;
long controlRevision=0;
bool controlEnabled=false,controlOK=false;
datetime controlResumeBar=0;
string eventQueue[];
datetime lastPublish=0,lastRecovery=0;

string Key(string name) { return ((name=="day" || name=="baseline" || name=="blocked")?accountPrefix:prefix)+name; }
double State(string name) { return GlobalVariableCheck(Key(name)) ? GlobalVariableGet(Key(name)) : 0; }
bool Save(string name,double value) {
 if(GlobalVariableSet(Key(name),value)==0) { storageOK=false; Print("Cannot persist execution state; removing executor."); ExpertRemove(); return false; }
 GlobalVariablesFlush(); return true;
}
void Halt(string reason) { Save("halt",1); Record("halted",reason); }
bool AccountOK() {
 return AccountInfoInteger(ACCOUNT_TRADE_MODE)==ACCOUNT_TRADE_MODE_DEMO &&
 AccountInfoInteger(ACCOUNT_LOGIN)==DemoLogin && AccountInfoString(ACCOUNT_SERVER)==DemoServer && AccountInfoString(ACCOUNT_CURRENCY)=="USD" &&
 AccountInfoInteger(ACCOUNT_MARGIN_MODE)==ACCOUNT_MARGIN_MODE_RETAIL_HEDGING;
}
int OnInit() {
 if((TradeSymbol!="XAUUSDm" && TradeSymbol!="EURUSDm" && TradeSymbol!="USDJPYm") || !AccountOK() || _Symbol!=TradeSymbol || StringLen(BridgeToken)!=64 || CommissionPerLot<0 || SlippageAllowance<0) {
  Print("Requires pinned USD hedging DEMO login, XAUUSDm, EURUSDm or USDJPYm chart, bridge token and nonnegative cost assumptions."); return INIT_PARAMETERS_INCORRECT;
 }
 accountPrefix=StringFormat("GoldExec.%s.%I64d.",DemoServer,DemoLogin);
 prefix=accountPrefix+(TradeSymbol=="EURUSDm"?"EURUSDm.":TradeSymbol=="USDJPYm"?"USDJPYm.":"");
 if(TradeSymbol=="EURUSDm") {MinTargetDistance=0.0005;ProfitLockDistance=0.0002;Magic1=2602001;Magic2=2602002;
  if(SlippageAllowance>0.0001) {Print("EURUSD slippage allowance must be at most one pip; use 0.00001.");return INIT_PARAMETERS_INCORRECT;}
 }
 if(TradeSymbol=="USDJPYm") {MinTargetDistance=0.05;ProfitLockDistance=0.02;Magic1=2603001;Magic2=2603002;
  if(SlippageAllowance>0.01) {Print("USDJPY slippage allowance must be at most one pip; use 0.001.");return INIT_PARAMETERS_INCORRECT;}
 }
 lockfile=FileOpen(prefix+"lock",FILE_READ|FILE_WRITE|FILE_BIN); // No FILE_SHARE flags: single terminal instance.
 if(lockfile==INVALID_HANDLE) { Print("Another executor holds this account lock."); return INIT_FAILED; }
 startupBar=iTime(TradeSymbol,PERIOD_M3,0);
 // New symbols may still be downloading history. The timer waits for M3.
 trade.SetAsyncMode(false);
 trade.SetTypeFillingBySymbol(TradeSymbol);
 trade.SetDeviationInPoints((ulong)MathCeil(SlippageAllowance/SymbolInfoDouble(TradeSymbol,SYMBOL_POINT)));
 LoadEvents();
 if(!EventSetTimer(1)) return INIT_FAILED;
 Record("started","Executor 1.108 loaded; reconciling broker positions and persistent state.");
 Print("Demo executor ready. New entries: ",EnableEntries,"; lots 2 x 0.06; pair risk $200; daily gate $1000.");
 return INIT_SUCCEEDED;
}
void OnDeinit(const int reason) { EventKillTimer(); if(lockfile!=INVALID_HANDLE) FileClose(lockfile); }
void UpdateDay() {
 long day=(long)TimeCurrent()/86400;
 if(State("day")!=day) {
  // Commit day last: an interrupted reset is repeated on restart.
  if(!Save("baseline",AccountInfoDouble(ACCOUNT_EQUITY))) return;
  if(!Save("blocked",0)) return;
  Save("day",(double)day);
 }
 if(State("baseline")-AccountInfoDouble(ACCOUNT_EQUITY)>=DailyLoss && State("blocked")==0) { Save("blocked",1); Record("daily_limit","Account equity loss reached the $1000 daily gate; new entries blocked."); }
}

// TP1 must actually close by broker TP, not merely disappear or touch a price.
bool TP1Closed() {
 ulong pid=(ulong)State("pid1");
 if(pid==0 || !HistorySelectByPosition(pid)) return false;
 double volume=0;
 for(int i=0;i<HistoryDealsTotal();i++) {
  ulong d=HistoryDealGetTicket(i);
  if(HistoryDealGetInteger(d,DEAL_ENTRY)==DEAL_ENTRY_OUT && HistoryDealGetInteger(d,DEAL_REASON)==DEAL_REASON_TP)
   volume+=HistoryDealGetDouble(d,DEAL_VOLUME);
 }
 return volume>=Lots-0.00000001;
}
void ManagePositions() {
 if(!TP1Closed()) return;
 for(int i=PositionsTotal()-1;i>=0;i--) {
  ulong ticket=PositionGetTicket(i);
  if(PositionGetString(POSITION_SYMBOL)!=TradeSymbol || (ulong)PositionGetInteger(POSITION_MAGIC)!=Magic2 ||
    (ulong)PositionGetInteger(POSITION_IDENTIFIER)!=(ulong)State("pid2")) continue;
  double entry=PositionGetDouble(POSITION_PRICE_OPEN), sl=PositionGetDouble(POSITION_SL), tp=PositionGetDouble(POSITION_TP);
  bool buy=PositionGetInteger(POSITION_TYPE)==POSITION_TYPE_BUY;
  double ticksize=SymbolInfoDouble(TradeSymbol,SYMBOL_TRADE_TICK_SIZE);
  if(ticksize<=0) return;
  double lockedStop=NormalizeDouble(MathRound(ProfitLockStop(entry,buy,ProfitLockDistance)/ticksize)*ticksize,(int)SymbolInfoInteger(TradeSymbol,SYMBOL_DIGITS));
  if((buy && sl>=lockedStop) || (!buy && sl>0 && sl<=lockedStop)) continue;
  MqlTick q; if(!SymbolInfoTick(TradeSymbol,q)) return;
  double distance=MathMax(SymbolInfoInteger(TradeSymbol,SYMBOL_TRADE_STOPS_LEVEL),SymbolInfoInteger(TradeSymbol,SYMBOL_TRADE_FREEZE_LEVEL))*SymbolInfoDouble(TradeSymbol,SYMBOL_POINT);
  if((buy ? q.bid-lockedStop : lockedStop-q.ask)<=distance) return;
  if(!EnsureProtection(ticket,lockedStop,tp))
   Record("breakeven_pending",trade.ResultRetcodeDescription());
  else Record("breakeven_done","TP2 stop verified beyond actual entry into profit.");
 }
}

bool Reject(string why) { rejection=why; return false; }
bool Estimate(ENUM_ORDER_TYPE side,double stop,double &risk,double &entry) {
 rejection="";
 MqlTick q;
 if(!SymbolInfoTick(TradeSymbol,q) || q.bid<=0 || q.ask<q.bid) return Reject("Invalid or unavailable bid/ask quote");
 double age=MathAbs((double)(TimeCurrent()*1000-q.time_msc));
 if(age>5000) return Reject(StringFormat("Quote age %.0f ms exceeds 5000 ms",age));
 bool buy=side==ORDER_TYPE_BUY;
 entry=buy?q.ask:q.bid;
 double d=buy?1:-1;
 if((intentTP1-entry)*d<=0 || (intentTP2-intentTP1)*d<0) return Reject(StringFormat("Entry %.3f has reached TP1 %.3f, or targets are unordered (TP2 %.3f)",entry,intentTP1,intentTP2));
 if((intentTP1-entry)*d<MinTargetDistance-0.00000001) return Reject(StringFormat("TP1 distance %.3f below minimum %.3f",(intentTP1-entry)*d,MinTargetDistance));
 double point=SymbolInfoDouble(TradeSymbol,SYMBOL_POINT), step=SymbolInfoDouble(TradeSymbol,SYMBOL_VOLUME_STEP);
 if(step<=0 || Lots<SymbolInfoDouble(TradeSymbol,SYMBOL_VOLUME_MIN) || Lots>SymbolInfoDouble(TradeSymbol,SYMBOL_VOLUME_MAX) || MathAbs(Lots/step-MathRound(Lots/step))>0.000001) return Reject(StringFormat("Lots %.2f incompatible with broker volume rules; step %.3f",Lots,step));
 double distance=SymbolInfoInteger(TradeSymbol,SYMBOL_TRADE_STOPS_LEVEL)*point;
 if((buy?q.bid-stop:stop-q.ask)<=distance || (entry-stop)*d<=0) return Reject(StringFormat("Stop %.3f invalid for quote %.3f/%.3f; minimum distance %.3f",stop,q.bid,q.ask,distance));
 if((buy?intentTP1-q.bid:q.ask-intentTP1)<=distance) return Reject(StringFormat("TP1 %.3f too close to quote; minimum distance %.3f",intentTP1,distance));
 double loss=0,margin=0;
 if(MinPairRewardRisk<=0 || !MathIsValidNumber(MinPairRewardRisk)) return Reject("Minimum pair reward/risk must be positive");
 double assumedEntry=entry+d*SlippageAllowance;
 if(!OrderCalcProfit(side,TradeSymbol,Lots,assumedEntry,stop-d*SlippageAllowance,loss) ||
    !OrderCalcMargin(side,TradeSymbol,Lots,entry,margin)) return Reject(StringFormat("Broker risk/margin calculation failed: error %d",GetLastError()));
 risk=-2*loss+2*Lots*CommissionPerLot;
 double ticksize=SymbolInfoDouble(TradeSymbol,SYMBOL_TRADE_TICK_SIZE);
 if(ticksize<=0) return Reject("Invalid broker tick size");
 double tp1=MathRound(intentTP1/ticksize)*ticksize, tp2=MathRound(intentTP2/ticksize)*ticksize;
 double reward1=0,reward2=0;
 if(!OrderCalcProfit(side,TradeSymbol,Lots,assumedEntry,tp1,reward1) ||
    !OrderCalcProfit(side,TradeSymbol,Lots,assumedEntry,tp2,reward2)) return Reject(StringFormat("Broker target reward calculation failed: error %d",GetLastError()));
 double reward=reward1+reward2-2*Lots*CommissionPerLot;
 double remaining=DailyLoss-MathMax(0,State("baseline")-AccountInfoDouble(ACCOUNT_EQUITY));
 if(risk<=0 || risk>PairRisk) return Reject(StringFormat("Estimated pair loss $%.2f exceeds $%.2f limit or is invalid",risk,PairRisk));
 if(!PairRewardRiskPass(reward,risk,MinPairRewardRisk)) return Reject(StringFormat("Combined net targets $%.2f are below %.2fx full pair risk $%.2f",reward,MinPairRewardRisk,risk));
 if(risk>remaining) return Reject(StringFormat("Estimated loss $%.2f exceeds remaining daily budget $%.2f",risk,remaining));
 double available=AccountInfoDouble(ACCOUNT_MARGIN_FREE),required=2*margin+risk;
 if(available<required) return Reject(StringFormat("Free margin $%.2f below conservative requirement $%.2f",available,required));
 return true;
}

bool OpenLeg(ENUM_ORDER_TYPE side,double stop,int leg,ulong &pid,ulong &ticket) {
 double risk,entry;
 if(!Estimate(side,stop,risk,entry)) { Record("entry_rejected",rejection); return false; }
 trade.SetExpertMagicNumber(leg==1?Magic1:Magic2);
 double ticksize=SymbolInfoDouble(TradeSymbol,SYMBOL_TRADE_TICK_SIZE);
 if(ticksize<=0) return false;
 double d=side==ORDER_TYPE_BUY?1:-1;
 double target=MathRound((leg==1?intentTP1:intentTP2)/ticksize)*ticksize;
 // Broker SL and TP are part of the initial request; never open naked positions.
 bool sent=trade.PositionOpen(TradeSymbol,side,Lots,entry,stop,target,"GB"+IntegerToString((long)State("attempt")));
 Record("order_result",StringFormat("Leg %d: retcode=%u; %s",leg,trade.ResultRetcode(),trade.ResultRetcodeDescription()));
 if(!sent || trade.ResultRetcode()!=TRADE_RETCODE_DONE || trade.ResultDeal()==0) return false;
 if(!HistoryDealSelect(trade.ResultDeal())) return false;
 pid=(ulong)HistoryDealGetInteger(trade.ResultDeal(),DEAL_POSITION_ID);
 if(pid==0 || pid>9007199254740991) return false;
 for(int i=PositionsTotal()-1;i>=0;i--) {
  ticket=PositionGetTicket(i);
  if((ulong)PositionGetInteger(POSITION_IDENTIFIER)!=pid) continue;
  if(MathAbs(PositionGetDouble(POSITION_VOLUME)-Lots)>0.00000001 || PositionGetDouble(POSITION_SL)<=0 || PositionGetDouble(POSITION_TP)<=0) return false;
  if((intentTP1-PositionGetDouble(POSITION_PRICE_OPEN))*d<MinTargetDistance-0.00000001) { Record("entry_rejected","Fill left TP1 closer than the symbol minimum; reconcile incomplete pair"); return false; }
  // Preserve absolute structural targets; never shift them with the fill.

  if(!EnsureProtection(ticket,stop,target)) return false;
  return true;
 }
 return false;
}

// A failed poll revokes entry permission immediately. Position management
// runs before this network request and does not depend on dashboard permission.
bool RefreshControl() {
 controlOK=false;
 char data[],reply[];string response;
 int status=WebRequest("GET",BridgeURL+APIPath()+"/bridge/control","Authorization: Bearer "+BridgeToken+"\r\n",1500,data,reply,response);
 if(status!=200) return false;
 string fields[];
 if(StringSplit(CharArrayToString(reply,0,-1,CP_UTF8),',',fields)!=2) return false;
 long revision=StringToInteger(fields[0]);
 if(revision<=0 || IntegerToString(revision)!=fields[0] || (fields[1]!="0" && fields[1]!="1")) return false;
 bool enabled=fields[1]=="1";
 if(revision!=controlRevision) controlResumeBar=iTime(TradeSymbol,PERIOD_M3,0);
 controlRevision=revision;controlEnabled=enabled;controlOK=true;
 return true;
}

void OnTimer() {
 if(!AccountOK()) { Print("Account changed; executor removed."); ExpertRemove(); return; }
 if(!TerminalInfoInteger(TERMINAL_CONNECTED)) { controlOK=false; Publish(); return; }
 int dailyLock=FileOpen(accountPrefix+"cycle.lock",FILE_READ|FILE_WRITE|FILE_BIN);
 if(dailyLock!=INVALID_HANDLE) {UpdateDay();FileClose(dailyLock);}
 if(!storageOK) return;
 ReconcilePartial();
 RecoverIdentifiers();
 ManagePositions(); // Keep managing existing TP2 even when new entries are disabled.
 RefreshControl();
 Publish();
 if(!controlOK || !controlEnabled || !storageOK || !EnableEntries || State("recovery")!=0 || State("halt")!=0 || State("blocked")!=0 || !TerminalInfoInteger(TERMINAL_TRADE_ALLOWED) || !MQLInfoInteger(MQL_TRADE_ALLOWED)) return;
 if(startupBar<=0) {startupBar=iTime(TradeSymbol,PERIOD_M3,0);return;}
 // Serialize entries across symbols without blocking their position management.
 int entryLock=FileOpen(accountPrefix+"cycle.lock",FILE_READ|FILE_WRITE|FILE_BIN);
 if(entryLock==INVALID_HANDLE) return;
 TryEntry();
 FileClose(entryLock);
}
void TryEntry() {
 UpdateDay();
 if(!storageOK || State("blocked")!=0) return;
 // Account-wide gate: no overlap with manual orders or other bots.
 if(PositionsTotal()!=0 || OrdersTotal()!=0) return;
 if((GlobalVariableCheck(accountPrefix+"recovery") && GlobalVariableGet(accountPrefix+"recovery")!=0) ||
    (GlobalVariableCheck(accountPrefix+"EURUSDm.recovery") && GlobalVariableGet(accountPrefix+"EURUSDm.recovery")!=0) ||
    (GlobalVariableCheck(accountPrefix+"USDJPYm.recovery") && GlobalVariableGet(accountPrefix+"USDJPYm.recovery")!=0)) return;
 datetime current=iTime(TradeSymbol,PERIOD_M3,0);
 if(current<=controlResumeBar || current<=startupBar || TimeCurrent()-current>30 || TimeCurrent()<current) return;
 datetime bar=current-180;
 if(State("attempt")>=(double)bar) return;
 char data[],reply[]; string response;
 int status=WebRequest("GET",BridgeURL+APIPath()+"/bridge/intent","Authorization: Bearer "+BridgeToken+"\r\n",1500,data,reply,response);
 if(status!=200) { if(status!=204) Record("api_unavailable",StringFormat("Intent HTTP %d; terminal error %d",status,GetLastError())); return; }
 string fields[];
 if(StringSplit(CharArrayToString(reply,0,-1,CP_UTF8),',',fields)!=7) return;
 if((datetime)StringToInteger(fields[0])!=bar || (fields[1]!="BUY" && fields[1]!="SELL")) return;
 double stop=StringToDouble(fields[2]); if(!MathIsValidNumber(stop) || stop<=0) return;
 intentTP1=StringToDouble(fields[5]); intentTP2=StringToDouble(fields[6]);
 if(!MathIsValidNumber(intentTP1) || !MathIsValidNumber(intentTP2) || intentTP1<=0 || intentTP2<=0) return;
 ENUM_ORDER_TYPE side=fields[1]=="BUY"?ORDER_TYPE_BUY:ORDER_TYPE_SELL;
 if(!LocalSignal(bar,side,stop,(int)StringToInteger(fields[3]),StringToDouble(fields[4]))) return;
 if(!AccountOK() || iTime(TradeSymbol,PERIOD_M3,0)!=current || TimeCurrent()-current>30) return;
 double ticksize=SymbolInfoDouble(TradeSymbol,SYMBOL_TRADE_TICK_SIZE); if(ticksize<=0) return;
 stop=(side==ORDER_TYPE_BUY?MathFloor(stop/ticksize):MathCeil(stop/ticksize))*ticksize;
 double risk,entry;
 if(!Estimate(side,stop,risk,entry)) { Save("attempt",(double)bar); Record("signal_skipped",rejection); return; }
 long approvedRevision=controlRevision;
 if(!RefreshControl() || !controlEnabled || controlRevision!=approvedRevision) return;
 // Persist BEFORE sending. Any uncertain response/restart halts, never retries the signal.
 if(!Save("attempt",(double)bar) || !Save("halt",1) || !Save("pid1",0) || !Save("pid2",0) || !Save("started",(double)TimeCurrent()) || !Save("recovery",1)) return;
 ulong pid1=0,pid2=0,ticket1=0,ticket2=0;
 if(!OpenLeg(side,stop,1,pid1,ticket1)) { Halt("First leg unconfirmed. Recovery will reconcile and flatten confirmed positions; no entry retry."); return; }
 if(!Save("pid1",(double)pid1)) return;
 if(!OpenLeg(side,stop,2,pid2,ticket2)) {
  // No retry on unknown second-leg state. First leg retains broker SL/TP.
  Halt("Second leg unconfirmed. Recovery will flatten confirmed positions, then keep entries halted for review."); return;
 }
 if(!Save("pid2",(double)pid2)) return;
 // Check actual combined stop risk after both fills; adverse fills can breach estimate.
 double actualRisk=2*Lots*CommissionPerLot;
 for(int i=PositionsTotal()-1;i>=0;i--) {
  ulong t=PositionGetTicket(i);
  ulong id=(ulong)PositionGetInteger(POSITION_IDENTIFIER);
  if(id!=pid1 && id!=pid2) continue;
  double profit=0;
  if(!OrderCalcProfit(side,TradeSymbol,PositionGetDouble(POSITION_VOLUME),PositionGetDouble(POSITION_PRICE_OPEN),stop,profit)) { Halt("post-fill risk unavailable"); return; }
  actualRisk-=profit;
 }
 if(actualRisk>PairRisk) { Halt("fill exceeded risk budget; protected positions require review"); return; }
 if(!Save("recovery",0)) return;
 Save("halt",0);
 Record("pair_opened",StringFormat("Two protected 0.06-lot positions; estimated combined risk $%.2f",risk));
}

// Recompute against this terminal/account so another bridge cannot supply an
// otherwise fresh signal from a different quote feed.
bool LocalSignal(datetime bar,ENUM_ORDER_TYPE side,double stop,int lookback,double buffer) {
 if((lookback!=10 && lookback!=20) || !MathIsValidNumber(buffer) || buffer<=0) return false;
 MqlRates r[]; ArraySetAsSeries(r,false);
 MqlRates history[]; ArraySetAsSeries(history,false);
 int count=CopyRates(TradeSymbol,PERIOD_M3,1,200,history);
 if(count<lookback+1) return false;
 ArrayResize(r,lookback+1);
 for(int k=0;k<=lookback;k++) r[k]=history[count-lookback-1+k];
 if(r[lookback].time!=bar || !LocalTargets(history,count,r[lookback].close,side)) return false;
 double high=r[0].high,low=r[0].low;
 for(int i=1;i<=lookback;i++) {
  if(r[i].time-r[i-1].time!=180) return false;
  if(i<lookback) { high=MathMax(high,r[i].high); low=MathMin(low,r[i].low); }
 }
 if(side==ORDER_TYPE_BUY && r[lookback].close<=high) return false;
 if(side==ORDER_TYPE_SELL && r[lookback].close>=low) return false;
 for(int i=lookback-1;i>=1;i--) {
  if(side==ORDER_TYPE_BUY && r[i].low<r[i-1].low && r[i].low<r[i+1].low) return MathAbs(stop-(r[i].low-buffer))<0.0000001;
  if(side==ORDER_TYPE_SELL && r[i].high>r[i-1].high && r[i].high>r[i+1].high) return MathAbs(stop-(r[i].high+buffer))<0.0000001;
 }
 return false;
}

// A matching existing SL/TP is already success. NO_CHANGES is accepted only
// after reselecting the exact ticket and verifying both broker-side levels.
bool ProtectionMatches(ulong ticket,double stop,double target) {
 if(!PositionSelectByTicket(ticket) || PositionGetString(POSITION_SYMBOL)!=TradeSymbol) return false;
 double ticksize=SymbolInfoDouble(TradeSymbol,SYMBOL_TRADE_TICK_SIZE);
 if(ticksize<=0 || stop<=0 || target<=0) return false;
 double tolerance=ticksize*0.1;
 return MathAbs(PositionGetDouble(POSITION_SL)-stop)<=tolerance &&
        MathAbs(PositionGetDouble(POSITION_TP)-target)<=tolerance;
}
bool EnsureProtection(ulong ticket,double stop,double target) {
 if(ProtectionMatches(ticket,stop,target)) return true;
 trade.PositionModify(ticket,stop,target);
 uint code=trade.ResultRetcode();
 if(code!=TRADE_RETCODE_DONE && code!=TRADE_RETCODE_NO_CHANGES) return false;
 return ProtectionMatches(ticket,stop,target);
}

bool LocalTargets(MqlRates &bars[],int count,double entry,ENUM_ORDER_TYPE side) {
 double first=0,second=0;
 bool buy=side==ORDER_TYPE_BUY;
 for(int i=1;i<count-1;i++) {
  if(bars[i].time-bars[i-1].time!=180 || bars[i+1].time-bars[i].time!=180) continue;
  double price=buy?bars[i].high:bars[i].low;
  if(buy) { if(price-entry<MinTargetDistance-0.00000001 || price<=bars[i-1].high || price<=bars[i+1].high) continue; }
  else { if(entry-price<MinTargetDistance-0.00000001 || price>=bars[i-1].low || price>=bars[i+1].low) continue; }
  if(price==first || price==second) continue;
  if(first==0 || (buy && price<first) || (!buy && price>first)) {second=first;first=price;}
  else if(second==0 || (buy && price<second) || (!buy && price>second)) second=price;
 }
 if(second==0) second=first;
 return first>0 && second>0 && MathAbs(intentTP1-first)<0.0000001 && MathAbs(intentTP2-second)<0.0000001;
}

string Escape(string s) {
 StringReplace(s,"\\","\\\\");StringReplace(s,"\"","\\\"");StringReplace(s,"\r","\\r");StringReplace(s,"\n","\\n");StringReplace(s,"\t","\\t");return s;
}
string Bool(bool v) { return v?"true":"false"; }
bool Owned() { ulong m=(ulong)PositionGetInteger(POSITION_MAGIC);return PositionGetString(POSITION_SYMBOL)==TradeSymbol && (m==Magic1 || m==Magic2); }
void StoreEvents() {
 int f=FileOpen(prefix+"events.tmp",FILE_WRITE|FILE_TXT|FILE_ANSI,0,CP_UTF8);
 if(f==INVALID_HANDLE) { storageOK=false;Save("halt",1);Print("Event outbox write failed; entries halted.");return; }
 for(int i=0;i<ArraySize(eventQueue);i++) FileWriteString(f,eventQueue[i]+"\n");
 FileFlush(f);FileClose(f);
 if(!FileMove(prefix+"events.tmp",0,prefix+"events.jsonl",FILE_REWRITE)) {storageOK=false;Save("halt",1);Print("Cannot commit event outbox; entries halted.");}
}
void LoadEvents() {
 int f=FileOpen(prefix+"events.jsonl",FILE_READ|FILE_TXT|FILE_ANSI,0,CP_UTF8);
 if(f==INVALID_HANDLE) return;
 while(!FileIsEnding(f)) {
  string line=FileReadString(f);if(StringLen(line)==0) continue;
  int n=ArraySize(eventQueue);if(n>=50) break;
  ArrayResize(eventQueue,n+1);eventQueue[n]=line;
 }
 FileClose(f);
}
void Record(string code,string detail) {
 static string previous="";static datetime at=0;
 string signature=code+detail;
 if(signature==previous && TimeLocal()-at<30) return;
 previous=signature;at=TimeLocal();
 Print(code,": ",detail);
 double sequence=State("eventseq")+1;if(!Save("eventseq",sequence)) return;
 string json=StringFormat("{\"id\":\"%I64d-%I64d\",\"time\":%I64d,\"code\":\"%s\",\"detail\":\"%s\"}",(long)TimeLocal(),(long)sequence,(long)TimeCurrent(),Escape(code),Escape(detail));
 int n=ArraySize(eventQueue);
 if(n>=50) { Print("Execution outbox full; oldest event retained only in terminal log.");for(int i=1;i<n;i++) eventQueue[i-1]=eventQueue[i];n--; }
 ArrayResize(eventQueue,n+1);eventQueue[n]=json;StoreEvents();
}

// Recovery never reissues a market entry. A persisted incomplete attempt is
// compensated by closing ONLY its positively identified broker positions.
void ReconcilePartial() {
 if(State("recovery")!=1 || TimeLocal()-lastRecovery<5) return;
 lastRecovery=TimeLocal();
 if(TimeCurrent()-(datetime)State("started")<10) return;
 if(!HistorySelect((datetime)State("started")-5,TimeCurrent())) {Record("recovery_wait","Broker history unavailable; keeping original protection and halt.");return;}
 string tag="GB"+IntegerToString((long)State("attempt"));
 ulong ids[];ArrayResize(ids,0);
 for(int i=0;i<HistoryDealsTotal();i++) {
  ulong deal=HistoryDealGetTicket(i),magic=(ulong)HistoryDealGetInteger(deal,DEAL_MAGIC);
  if(HistoryDealGetString(deal,DEAL_SYMBOL)!=TradeSymbol || (magic!=Magic1 && magic!=Magic2) || HistoryDealGetInteger(deal,DEAL_ENTRY)!=DEAL_ENTRY_IN) continue;
  if(HistoryDealGetString(deal,DEAL_COMMENT)!=tag) continue;
  int n=ArraySize(ids);ArrayResize(ids,n+1);ids[n]=(ulong)HistoryDealGetInteger(deal,DEAL_POSITION_ID);
  if(ids[n]<=9007199254740991) Save(magic==Magic1?"pid1":"pid2",(double)ids[n]);
 }
 // Pending/accepted orders may still fill later: keep halted and keep reconciling.
 int pending=0;
 for(int i=0;i<OrdersTotal();i++) {ulong t=OrderGetTicket(i);ulong m=(ulong)OrderGetInteger(ORDER_MAGIC);if(OrderGetString(ORDER_SYMBOL)==TradeSymbol && (m==Magic1 || m==Magic2)) pending++;}
 int live=0;
 for(int i=PositionsTotal()-1;i>=0;i--) {
  ulong ticket=PositionGetTicket(i);if(!Owned()) continue;
  ulong id=(ulong)PositionGetInteger(POSITION_IDENTIFIER);
  bool match=PositionGetString(POSITION_COMMENT)==tag;
  for(int j=0;j<ArraySize(ids);j++) if(ids[j]==id) match=true;
  if(id==(ulong)State("pid1") || id==(ulong)State("pid2")) match=true;
  if(!RecoveryOwns(true,true,match,false)) {Record("recovery_unmatched","An owned position cannot be linked to the interrupted pair; manual review required.");continue;}
  live++;
  if(RecoveryDecision(true,true,true,true,pending>0,TerminalInfoInteger(TERMINAL_TRADE_ALLOWED) && MQLInfoInteger(MQL_TRADE_ALLOWED))!=RECOVERY_CLOSE) {Record("recovery_wait","Enable algorithmic trading to flatten interrupted pair; new entries remain halted.");continue;}
  bool result=trade.PositionClose(ticket);
  Record("recovery_close",StringFormat("Ticket %I64u close request: %s; retcode %u (%s). Broker position will be rechecked.",ticket,Bool(result),trade.ResultRetcode(),trade.ResultRetcodeDescription()));
 }
 if(RecoveryDecision(true,true,true,live>0,pending>0,true)==RECOVERY_REVIEW) Record("recovery_flat","No identified open legs or pending bot orders. Entries remain halted; reconcile history before manual reset. No entry retries were sent.");
}

void Publish() {
 if(TimeLocal()-lastPublish<5) return;
 lastPublish=TimeLocal();
 string positions="[";int count=0;
 for(int i=0;i<PositionsTotal();i++) {
  ulong ticket=PositionGetTicket(i);if(!Owned()) continue;
  if(count++>0) positions+=",";
  positions+=StringFormat("{\"ticket\":\"%I64u\",\"identifier\":\"%I64d\",\"leg\":%d,\"side\":\"%s\",\"lots\":%.8f,\"entry\":%.8f,\"sl\":%.8f,\"tp\":%.8f,\"profit\":%.8f}",ticket,PositionGetInteger(POSITION_IDENTIFIER),PositionGetInteger(POSITION_MAGIC)==(long)Magic1?1:2,PositionGetInteger(POSITION_TYPE)==POSITION_TYPE_BUY?"BUY":"SELL",PositionGetDouble(POSITION_VOLUME),PositionGetDouble(POSITION_PRICE_OPEN),PositionGetDouble(POSITION_SL),PositionGetDouble(POSITION_TP),PositionGetDouble(POSITION_PROFIT)+PositionGetDouble(POSITION_SWAP));
 }
 positions+="]";
 bool history=HistorySelect(TimeCurrent()-7*86400,TimeCurrent());
 string deals="[";count=0;
 // Include manual closing deals by matching position IDs from bot entries.
 ulong ids[];int legs[];ArrayResize(ids,0);ArrayResize(legs,0);
 if(history) for(int i=0;i<HistoryDealsTotal();i++) {
  ulong t=HistoryDealGetTicket(i);long m=HistoryDealGetInteger(t,DEAL_MAGIC);
  if(HistoryDealGetString(t,DEAL_SYMBOL)!=TradeSymbol || (m!=(long)Magic1 && m!=(long)Magic2)) continue;
  int n=ArraySize(ids);ArrayResize(ids,n+1);ArrayResize(legs,n+1);ids[n]=(ulong)HistoryDealGetInteger(t,DEAL_POSITION_ID);legs[n]=m==(long)Magic1?1:2;
 }
 if(history) for(int i=HistoryDealsTotal()-1;i>=0 && count<200;i--) {
  ulong t=HistoryDealGetTicket(i),pid=(ulong)HistoryDealGetInteger(t,DEAL_POSITION_ID);
  if(HistoryDealGetString(t,DEAL_SYMBOL)!=TradeSymbol) continue;
  int leg=pid==(ulong)State("pid1")?1:pid==(ulong)State("pid2")?2:0;
  for(int j=0;j<ArraySize(ids);j++) if(ids[j]==pid) {leg=legs[j];break;}
  if(leg==0) continue;
  if(count++>0) deals+=",";
  double net=HistoryDealGetDouble(t,DEAL_PROFIT)+HistoryDealGetDouble(t,DEAL_COMMISSION)+HistoryDealGetDouble(t,DEAL_SWAP)+HistoryDealGetDouble(t,DEAL_FEE);
  deals+=StringFormat("{\"ticket\":\"%I64u\",\"identifier\":\"%I64u\",\"time\":%I64d,\"leg\":%d,\"entry\":%d,\"lots\":%.8f,\"price\":%.8f,\"net\":%.8f,\"reason\":\"%s\"}",t,pid,HistoryDealGetInteger(t,DEAL_TIME),leg,(int)HistoryDealGetInteger(t,DEAL_ENTRY),HistoryDealGetDouble(t,DEAL_VOLUME),HistoryDealGetDouble(t,DEAL_PRICE),net,EnumToString((ENUM_DEAL_REASON)HistoryDealGetInteger(t,DEAL_REASON)));
 }
 deals+="]";
 string events="[";for(int i=0;i<ArraySize(eventQueue);i++) {if(i>0) events+=",";events+=eventQueue[i];}events+="]";
 double change=AccountInfoDouble(ACCOUNT_EQUITY)-State("baseline"),remaining=State("blocked")!=0?0:MathMax(0,DailyLoss-MathMax(0,-change));
 string body=StringFormat("{\"demo\":true,\"account\":\"%I64d\",\"server\":\"%s\",\"version\":\"1.108\",\"broker_time\":%I64d,\"connected\":%s,\"enabled\":%s,\"halted\":%s,\"daily_blocked\":%s,\"recovering\":%s,\"history_ok\":%s,\"daily_remaining\":%.8f,\"day_equity_change\":%.8f,\"positions\":%s,\"deals\":%s,\"events\":%s}",DemoLogin,Escape(DemoServer),(long)TimeCurrent(),Bool(TerminalInfoInteger(TERMINAL_CONNECTED)),Bool(EnableEntries && TerminalInfoInteger(TERMINAL_TRADE_ALLOWED) && MQLInfoInteger(MQL_TRADE_ALLOWED)),Bool(State("halt")!=0),Bool(State("blocked")!=0),Bool(State("recovery")!=0),Bool(history),remaining,change,positions,deals,events);
 body=StringSubstr(body,0,StringLen(body)-1)+",\"symbol\":\""+TradeSymbol+"\""+StringFormat(",\"control_revision\":%I64d,\"control_enabled\":%s,\"control_ok\":%s}",controlRevision,Bool(controlEnabled),Bool(controlOK));
 char data[],reply[];int size=StringToCharArray(body,data,0,WHOLE_ARRAY,CP_UTF8);if(size<=0) return;ArrayResize(data,size-1);string headers;
 int status=WebRequest("POST",BridgeURL+APIPath()+"/bridge/execution","Content-Type: application/json\r\nAuthorization: Bearer "+BridgeToken+"\r\n",1500,data,reply,headers);
 if(status==200) {ArrayResize(eventQueue,0);StoreEvents();}
 else PrintFormat("Execution telemetry HTTP %d; queued events retained",status);
}

// Recover IDs from tagged entry deals if a restart occurred between broker fill
// and saving the local identifier. Never invent a new signal or reopen a leg.
void RecoverIdentifiers() {
 if(State("attempt")<=0 || (State("pid1")>0 && State("pid2")>0)) return;
 static datetime checked=0;if(TimeLocal()-checked<10) return;checked=TimeLocal();
 if(!HistorySelect((datetime)State("attempt"),TimeCurrent())) return;
 string tag="GB"+IntegerToString((long)State("attempt"));
 for(int i=0;i<HistoryDealsTotal();i++) {
  ulong ticket=HistoryDealGetTicket(i);long magic=HistoryDealGetInteger(ticket,DEAL_MAGIC);
  if(HistoryDealGetString(ticket,DEAL_SYMBOL)!=TradeSymbol || HistoryDealGetString(ticket,DEAL_COMMENT)!=tag || HistoryDealGetInteger(ticket,DEAL_ENTRY)!=DEAL_ENTRY_IN || (magic!=(long)Magic1 && magic!=(long)Magic2)) continue;
  ulong id=(ulong)HistoryDealGetInteger(ticket,DEAL_POSITION_ID);string key=magic==(long)Magic1?"pid1":"pid2";
  if(id>0 && id<=9007199254740991 && State(key)==0) {if(Save(key,(double)id)) Record("identifier_recovered",StringFormat("Recovered %s broker position %I64u from history",key,id));}
 }
}
