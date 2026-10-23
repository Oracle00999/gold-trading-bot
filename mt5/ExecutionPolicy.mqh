// Kept within the MQL5/C++ common subset for executable policy regression tests.
const int RECOVERY_NONE=0, RECOVERY_WAIT=1, RECOVERY_CLOSE=2, RECOVERY_REVIEW=3;
bool RecoveryOwns(bool symbolMatches,bool magicMatches,bool commentMatches,bool identifierMatches) {
 return symbolMatches && magicMatches && (commentMatches || identifierMatches);
}
int RecoveryDecision(bool incomplete,bool connected,bool historyReady,bool matchingLive,bool pending,bool allowed) {
 if(!incomplete) return RECOVERY_NONE;
 if(!connected || !historyReady) return RECOVERY_WAIT;
 if(matchingLive) return allowed?RECOVERY_CLOSE:RECOVERY_WAIT;
 if(pending) return RECOVERY_WAIT;
 return RECOVERY_REVIEW; // Never infer that an uncertain order was rejected.
}

// Price units, not account-currency profit.
double ProfitLockStop(double entry,bool buy,double distance=2.0) { return entry+(buy?distance:-distance); }

// Compare the combined net value of both structural targets with the full
// two-leg stop loss. Both values are in account currency.
bool PairRewardRiskPass(double reward,double risk,double minimum=0.90) {
 return reward>0 && risk>0 && minimum>0 && reward+0.00000001>=risk*minimum;
}
