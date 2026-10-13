#include <cassert>
#include "../mt5/ExecutionPolicy.mqh"
int main() {
 assert(ProfitLockStop(4100,false)==4098);
 assert(ProfitLockStop(4100,true)==4102);
 assert(RecoveryDecision(false,true,true,true,false,true)==RECOVERY_NONE); // completed pair survives restart
 assert(RecoveryDecision(true,true,true,true,false,true)==RECOVERY_CLOSE); // confirmed orphan
 assert(RecoveryDecision(true,false,true,true,false,true)==RECOVERY_WAIT); // disconnected
 assert(RecoveryDecision(true,true,false,true,false,true)==RECOVERY_WAIT); // missing history
 assert(RecoveryDecision(true,true,true,true,false,false)==RECOVERY_WAIT); // trading permission off
 assert(RecoveryDecision(true,true,true,false,true,true)==RECOVERY_WAIT); // order may still fill
 assert(RecoveryDecision(true,true,true,false,false,true)==RECOVERY_REVIEW); // flat isn't proof of rejection
 assert(!RecoveryOwns(true,false,true,true)); // manual trade
 assert(!RecoveryOwns(false,true,true,true)); // other symbol
 assert(!RecoveryOwns(true,true,false,false)); // unrelated pair
 assert(RecoveryOwns(true,true,false,true)); // persisted broker identifier after restart
 assert(RecoveryOwns(true,true,true,false)); // fill before local ID was saved
}
