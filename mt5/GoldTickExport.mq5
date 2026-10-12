#property strict
#property version "1.000"
#property script_show_inputs
input int DaysBack=7;
string Quote(string s){StringReplace(s,"\\","\\\\");StringReplace(s,"\"","\\\"");return "\""+s+"\"";}
void OnStart(){
 if(AccountInfoInteger(ACCOUNT_TRADE_MODE)!=ACCOUNT_TRADE_MODE_DEMO||DaysBack<1||DaysBack>31){Print("Use demo and 1–31 days");return;}
 string symbol="XAUUSDm";if(!SymbolSelect(symbol,true))return;
 ulong end=(ulong)TimeCurrent()*1000, start=end-(ulong)DaysBack*86400000;
 string name="GoldTicks-"+IntegerToString((long)TimeLocal())+".jsonl";
 int f=FileOpen(name,FILE_WRITE|FILE_TXT|FILE_ANSI,0,CP_UTF8);if(f==INVALID_HANDLE)return;
 FileWriteString(f,StringFormat("{\"type\":\"metadata\",\"symbol\":\"XAUUSDm\",\"server\":%s,\"demo\":true,\"from\":%I64u,\"to\":%I64u,\"contract_size\":%.8f,\"profit_currency\":%s}\n",Quote(AccountInfoString(ACCOUNT_SERVER)),start,end,SymbolInfoDouble(symbol,SYMBOL_TRADE_CONTRACT_SIZE),Quote(SymbolInfoString(symbol,SYMBOL_CURRENCY_PROFIT))));
 long total=0;int failures=0;
 for(ulong from=start;from<=end&&!IsStopped();from+=3600000){
  ulong to=from+3599999;if(to>end)to=end;
  MqlTick ticks[];ResetLastError();int n=CopyTicksRange(symbol,ticks,COPY_TICKS_ALL,from,to);int err=GetLastError();
  if(n<0||err!=0){failures++;PrintFormat("Tick chunk incomplete, error %d; rerun after synchronization",err);}
  for(int i=0;i<n;i++){FileWriteString(f,StringFormat("{\"type\":\"tick\",\"time\":%I64d,\"bid\":%.8f,\"ask\":%.8f}\n",ticks[i].time_msc,ticks[i].bid,ticks[i].ask));total++;}
 }
 FileWriteString(f,StringFormat("{\"type\":\"end\",\"count\":%I64d,\"failures\":%d,\"cancelled\":%s}\n",total,failures,IsStopped()?"true":"false"));FileClose(f);
 PrintFormat("GoldTickExport: %I64d ticks, %d failed chunks. MQL5/Files/%s",total,failures,name);
}
