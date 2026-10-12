#property strict
#property version "1.000"
#property script_show_inputs
input int RequestedBars = 50000;
string Q(string s){StringReplace(s,"\\","\\\\");StringReplace(s,"\"","\\\"");return "\""+s+"\"";}
void OnStart()
{
 if(AccountInfoInteger(ACCOUNT_TRADE_MODE)!=ACCOUNT_TRADE_MODE_DEMO){Print("Demo account required");return;}
 if(RequestedBars<22||RequestedBars>100000){Print("Request 22 to 100000 bars");return;}
 string symbol="XAUUSDm";
 if(!SymbolSelect(symbol,true))return;
 MqlRates rates[];ArraySetAsSeries(rates,false);
 int count=CopyRates(symbol,PERIOD_M3,1,RequestedBars,rates);
 if(count<=0){Print("History is loading; run GoldHistoryExport again shortly. Error ",GetLastError());return;}
 datetime current=iTime(symbol,PERIOD_M3,0);
 string filename="GoldHistory-"+IntegerToString((long)TimeLocal())+".json";
 int f=FileOpen(filename,FILE_WRITE|FILE_TXT|FILE_ANSI,0,CP_UTF8);
 if(f==INVALID_HANDLE){Print("Cannot create export: ",GetLastError());return;}
 string header=StringFormat("{\"demo\":true,\"symbol\":%s,\"server\":%s,\"timeframe\":\"M3\",\"requested_bars\":%d,\"current_bar_time\":%I64d,\"contract_size\":%.8f,\"volume_min\":%.8f,\"volume_max\":%.8f,\"volume_step\":%.8f,\"tick_size\":%.8f,\"profit_currency\":%s,\"candles\":[",Q(symbol),Q(AccountInfoString(ACCOUNT_SERVER)),RequestedBars,(long)current,SymbolInfoDouble(symbol,SYMBOL_TRADE_CONTRACT_SIZE),SymbolInfoDouble(symbol,SYMBOL_VOLUME_MIN),SymbolInfoDouble(symbol,SYMBOL_VOLUME_MAX),SymbolInfoDouble(symbol,SYMBOL_VOLUME_STEP),SymbolInfoDouble(symbol,SYMBOL_TRADE_TICK_SIZE),Q(SymbolInfoString(symbol,SYMBOL_CURRENCY_PROFIT)));
 FileWriteString(f,header);
 for(int i=0;i<count;i++){
  if(i>0)FileWriteString(f,",");
  FileWriteString(f,StringFormat("{\"time\":%I64d,\"open\":%.8f,\"high\":%.8f,\"low\":%.8f,\"close\":%.8f,\"tick_volume\":%I64d}",(long)rates[i].time,rates[i].open,rates[i].high,rates[i].low,rates[i].close,rates[i].tick_volume));
 }
 FileWriteString(f,"]}");FileClose(f);
 PrintFormat("Exported %d/%d closed M3 candles and contract specs to MQL5/Files/%s",count,RequestedBars,filename);
 if(count<RequestedBars)Print("Partial history only. Let MT5 download more history, then rerun. No gaps were filled.");
}
