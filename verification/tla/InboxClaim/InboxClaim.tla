---- MODULE InboxClaim ----
EXTENDS Naturals, FiniteSets

Rows == 1..3
Tokens == 1..2
OrderingKey(i) == IF i = 3 THEN "other" ELSE "room"
Lease == 2
LastTime == 2

VARIABLES inbox, calls, now, explicitRecovery, releaseExact, completionFenced,
          reclaimFenced, renewalFenced

Init ==
    /\ inbox = [i \in Rows |-> [status |-> "pending", token |-> 0, leaseUntil |-> 0, attempts |-> 0]]
    /\ calls = [t \in Tokens |-> [phase |-> "unused", ids |-> {}]]
    /\ now = 0
    /\ explicitRecovery \in BOOLEAN
    /\ releaseExact = TRUE /\ completionFenced = TRUE
    /\ reclaimFenced = TRUE /\ renewalFenced = TRUE

IsHead(i) ==
    /\ inbox[i].status \in {"pending", "processing"}
    /\ \A j \in Rows : (OrderingKey(j) = OrderingKey(i) /\ j < i) => inbox[j].status = "completed"

Eligible(i) ==
    \/ inbox[i].status = "pending"
    \/ /\ inbox[i].status = "processing" /\ ~explicitRecovery
       /\ inbox[i].leaseUntil <= now

Ready == {i \in Rows : IsHead(i) /\ Eligible(i)}
Candidates(limit) == {i \in Ready : Cardinality({j \in Ready : j < i}) < limit}

ClaimBatch(t, limit) ==
    LET batch == Candidates(limit) IN
    /\ calls[t].phase = "unused"
    /\ \A u \in Tokens : u < t => calls[u].phase # "unused"
    /\ batch # {}
    /\ inbox' = [i \in Rows |-> IF i \in batch
           THEN [inbox[i] EXCEPT !.status = "processing", !.token = t,
                                  !.leaseUntil = now + Lease, !.attempts = @ + 1]
           ELSE inbox[i]]
    /\ calls' = [calls EXCEPT ![t] = [phase |-> "reading", ids |-> batch]]
    /\ reclaimFenced' = (\A i \in batch : inbox[i].status = "pending" \/
              (~explicitRecovery /\ inbox[i].leaseUntil <= now))
    /\ UNCHANGED <<now, explicitRecovery, releaseExact, completionFenced, renewalFenced>>

ReturnClaims(t, keep) ==
    /\ calls[t].phase = "reading"
    /\ keep \subseteq calls[t].ids
    /\ calls' = [calls EXCEPT ![t] = [phase |-> "returned", ids |-> keep]]
    /\ UNCHANGED <<inbox, now, explicitRecovery, releaseExact, completionFenced,
                   reclaimFenced, renewalFenced>>

ReleaseClaimToken(t) ==
    LET keep == calls[t].ids
        matches(i) == inbox[i].status = "processing" /\ inbox[i].token = t /\ i \notin keep
    IN
    /\ calls[t].phase = "returned"
    /\ inbox' = [i \in Rows |-> IF matches(i)
           THEN [inbox[i] EXCEPT !.status = "pending", !.token = 0, !.leaseUntil = 0,
                                  !.attempts = IF @ > 0 THEN @ - 1 ELSE 0]
           ELSE inbox[i]]
    /\ releaseExact' = (\A i \in Rows :
           IF i \in keep \/ inbox[i].token # t \/ inbox[i].status # "processing"
           THEN inbox'[i] = inbox[i]
           ELSE /\ inbox'[i].status = "pending" /\ inbox'[i].token = 0
                /\ inbox'[i].leaseUntil = 0 /\ inbox'[i].attempts + 1 = inbox[i].attempts)
    /\ UNCHANGED <<now, calls, explicitRecovery, releaseExact, completionFenced, reclaimFenced, renewalFenced>>

HasClaim(t, i) == calls[t].phase = "returned" /\ i \in calls[t].ids

Complete(t, i) ==
    /\ HasClaim(t, i)
    /\ inbox[i].status = "processing" /\ inbox[i].token = t
    /\ inbox[i].leaseUntil > now
    /\ inbox' = [inbox EXCEPT ![i].status = "completed", ![i].token = 0, ![i].leaseUntil = 0]
    /\ completionFenced' = (inbox[i].token = t /\ now < inbox[i].leaseUntil)
    /\ UNCHANGED <<now, calls, explicitRecovery, releaseExact, reclaimFenced, renewalFenced>>

RenewInbox(t, i) ==
    /\ HasClaim(t, i)
    /\ inbox[i].status = "processing" /\ inbox[i].token = t
    /\ inbox' = [inbox EXCEPT ![i].leaseUntil = now + Lease]
    /\ renewalFenced' = (inbox[i].token = t)
    /\ UNCHANGED <<now, calls, explicitRecovery, releaseExact, completionFenced, reclaimFenced>>

Tick ==
    /\ now < LastTime /\ now' = now + 1
    /\ UNCHANGED <<inbox, calls, explicitRecovery, releaseExact, completionFenced,
                   reclaimFenced, renewalFenced>>

Next ==
    \/ \E t \in Tokens, n \in 1..2 : ClaimBatch(t, n)
    \/ \E t \in Tokens, keep \in SUBSET Rows : ReturnClaims(t, keep)
    \/ \E t \in Tokens : ReleaseClaimToken(t)
    \/ \E t \in Tokens, i \in Rows : Complete(t, i)
    \/ \E t \in Tokens, i \in Rows : RenewInbox(t, i)
    \/ Tick

OrderingKeyFIFO == inbox[2].status # "pending" => inbox[1].status = "completed"
ReleaseOnlyUnreturned == releaseExact
CompleteOnlyLiveOwner == completionFenced
ReclaimOnlyExpiredImplicit == reclaimFenced
RenewOnlyCurrentOwner == renewalFenced
ExpiredCurrentOwnerCanRenew ==
    \A t \in Tokens, i \in Rows :
        (HasClaim(t, i) /\ inbox[i].status = "processing" /\ inbox[i].token = t
                        /\ inbox[i].leaseUntil <= now) => ENABLED RenewInbox(t, i)
====
