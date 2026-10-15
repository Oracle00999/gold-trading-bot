#property strict
#property version "1.101"
#property description "Demo-only Gold/EURUSD telemetry bridge. Does not place orders."

input string BridgeURL = "http://127.0.0.1:8080";
input string BridgeToken = "";
input string GoldSymbol = "XAUUSDm";

string APIPath() {return GoldSymbol=="EURUSDm"?"/api/pairs/EURUSDm":"/api";}
int previous_status = 0;

int candle_status = 0;

int OnInit()
{
   if(AccountInfoInteger(ACCOUNT_TRADE_MODE) != ACCOUNT_TRADE_MODE_DEMO)
   {
      Print("GoldBridge requires a demo account.");
      return INIT_FAILED;
   }
   if(StringLen(BridgeToken) != 64 || (GoldSymbol != "XAUUSDm" && GoldSymbol != "EURUSDm"))
   {
      Print("Set BridgeToken from the Go application's .local/bridge-token file.");
      return INIT_PARAMETERS_INCORRECT;
   }
   if(!SymbolSelect(GoldSymbol, true)) return INIT_FAILED;
   if(!EventSetTimer(2)) return INIT_FAILED;
   Print("GoldBridge started: telemetry only, no orders.");
   return INIT_SUCCEEDED;
}

void OnDeinit(const int reason) { EventKillTimer(); }

void OnTimer()
{
   if(AccountInfoInteger(ACCOUNT_TRADE_MODE) != ACCOUNT_TRADE_MODE_DEMO)
   {
      Print("Demo account required; stopping bridge.");
      ExpertRemove();
      return;
   }
   MqlTick tick;
   if(!SymbolInfoTick(GoldSymbol, tick) || tick.bid <= 0 || tick.ask <= 0) return;
   string currency = AccountInfoString(ACCOUNT_CURRENCY);
   if(StringLen(currency) != 3) return;
   string body = StringFormat(
      "{\"demo\":true,\"connected\":%s,\"currency\":\"%s\",\"balance\":%.8f,\"equity\":%.8f,\"symbol\":\"%s\",\"bid\":%.8f,\"ask\":%.8f,\"tick_time_ms\":%I64d}",
      TerminalInfoInteger(TERMINAL_CONNECTED) ? "true" : "false",
      currency, AccountInfoDouble(ACCOUNT_BALANCE), AccountInfoDouble(ACCOUNT_EQUITY), GoldSymbol, tick.bid, tick.ask, tick.time_msc);
   if(TerminalInfoInteger(TERMINAL_CONNECTED))
      for(int frame=0; frame<4; frame++) SendCandles(frame);
   char data[], result[];
   int size = StringToCharArray(body, data, 0, WHOLE_ARRAY, CP_UTF8);
   if(size <= 0) return;
   ArrayResize(data, size - 1);
   string headers = "Content-Type: application/json\r\nAuthorization: Bearer " + BridgeToken + "\r\n";
   string response_headers;
   ResetLastError();
   int status = WebRequest("POST", BridgeURL + APIPath() + "/bridge/snapshot", headers, 1500, data, result, response_headers);
   if(status != previous_status)
   {
      if(status == 200) Print("GoldBridge connected to Go API.");
      else PrintFormat("GoldBridge HTTP status %d; terminal error %d. Check API, token and WebRequest allowlist.", status, GetLastError());
      previous_status = status;
   }
}

// CopyRates shift 1 excludes the forming candle. Physical order is oldest first.
void SendCandles(int index)
{
   ENUM_TIMEFRAMES frames[4] = {PERIOD_M3, PERIOD_M5, PERIOD_M15, PERIOD_H1};
   string names[4] = {"M3", "M5", "M15", "H1"};
   MqlRates rates[];
   ArraySetAsSeries(rates, false);
   datetime current = iTime(GoldSymbol, frames[index], 0);
   int count = CopyRates(GoldSymbol, frames[index], 1, 200, rates);
   if(count <= 0 || current <= 0) return; // Retry after terminal downloads history.
   string body = StringFormat("{\"demo\":true,\"symbol\":\"%s\",\"timeframe\":\"%s\",\"current_bar_time\":%I64d,\"candles\":[", GoldSymbol, names[index], (long)current);
   for(int i=0; i<count; i++)
   {
      if(i>0) body += ",";
      body += StringFormat("{\"time\":%I64d,\"open\":%.8f,\"high\":%.8f,\"low\":%.8f,\"close\":%.8f,\"tick_volume\":%I64d}", (long)rates[i].time, rates[i].open, rates[i].high, rates[i].low, rates[i].close, rates[i].tick_volume);
   }
   MqlRates forming[];
   if(CopyRates(GoldSymbol, frames[index], 0, 1, forming) != 1 || forming[0].time != current) return;
   body += StringFormat("],\"forming\":{\"time\":%I64d,\"open\":%.8f,\"high\":%.8f,\"low\":%.8f,\"close\":%.8f,\"tick_volume\":%I64d}}", (long)forming[0].time, forming[0].open, forming[0].high, forming[0].low, forming[0].close, forming[0].tick_volume);
   char data[], result[];
   int size=StringToCharArray(body,data,0,WHOLE_ARRAY,CP_UTF8);
   if(size<=0) return;
   ArrayResize(data,size-1);
   string response_headers;
   int status=WebRequest("POST",BridgeURL+APIPath()+"/bridge/candles","Content-Type: application/json\r\nAuthorization: Bearer "+BridgeToken+"\r\n",1500,data,result,response_headers);
   if(status!=candle_status)
   {
      PrintFormat("GoldBridge candle upload HTTP %d (%s, %d closed candles)",status,names[index],count);
      candle_status=status;
   }
}
