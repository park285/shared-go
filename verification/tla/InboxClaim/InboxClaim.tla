---- MODULE InboxClaim ----
EXTENDS Naturals, FiniteSets

CONSTANT SecondAdmissionTimes

Rows == 1..3
Tokens == 1..2
OrderingKey(i) == IF i = 3 THEN "other" ELSE "room"
Lease == 2
LastTime == 2

VARIABLES inbox, calls, now, explicitRecovery, releaseExact, completionFenced,
          reclaimFenced, renewalFenced

Init ==
    /\ \E secondTime \in SecondAdmissionTimes :
        inbox = [i \in Rows |-> [status |-> IF i = 2 THEN "private" ELSE "pending",
                                 createdAt |-> IF i = 2 THEN secondTime ELSE 1, availableAt |-> 0,
                                 token |-> 0, leaseUntil |-> 0, attempts |-> 0]]
    /\ calls = [t \in Tokens |-> [phase |-> "unused", ids |-> {}, priorityKept |-> TRUE]]
    /\ now = 0
    /\ explicitRecovery \in BOOLEAN
    /\ releaseExact = TRUE /\ completionFenced = TRUE
    /\ reclaimFenced = TRUE /\ renewalFenced = TRUE

CommitAdmission ==
    /\ inbox[2].status = "private"
    /\ inbox' = [inbox EXCEPT ![2].status = "pending"]
    /\ UNCHANGED <<calls, now, explicitRecovery, releaseExact, completionFenced,
                   reclaimFenced, renewalFenced>>

Active(i) == inbox[i].status \in {"pending", "processing"}
Precedes(j, i) == j < i

IsHead(i) ==
    /\ Active(i)
    /\ ~ \E j \in Rows : Active(j) /\ OrderingKey(j) = OrderingKey(i) /\ Precedes(j, i)

Eligible(i) ==
    /\ inbox[i].availableAt <= now
    /\ \/ inbox[i].status = "pending"
       \/ /\ inbox[i].status = "processing" /\ ~explicitRecovery
          /\ inbox[i].leaseUntil <= now

Ready == {i \in Rows : IsHead(i) /\ Eligible(i)}
HigherPriority(j, i) ==
    IF inbox[j].availableAt # inbox[i].availableAt THEN inbox[j].availableAt < inbox[i].availableAt
    ELSE IF inbox[j].createdAt # inbox[i].createdAt THEN inbox[j].createdAt < inbox[i].createdAt
    ELSE j < i

Candidates(limit) == {i \in Ready : Cardinality({j \in Ready : HigherPriority(j, i)}) < limit}

PriorityKept(batch) ==
    \A i \in batch, j \in Ready \ batch :
        LET chosen == inbox[i]
            missed == inbox[j]
        IN /\ chosen.availableAt <= missed.availableAt
           /\ (chosen.availableAt = missed.availableAt => chosen.createdAt <= missed.createdAt)
           /\ (chosen.availableAt = missed.availableAt /\ chosen.createdAt = missed.createdAt => i < j)

ClaimBatch(t, limit) ==
    LET batch == Candidates(limit) IN
    /\ calls[t].phase = "unused"
    /\ \A u \in Tokens : u < t => calls[u].phase # "unused"
    /\ batch # {}
    /\ inbox' = [i \in Rows |-> IF i \in batch
           THEN [inbox[i] EXCEPT !.status = "processing", !.token = t,
                                  !.leaseUntil = now + Lease, !.attempts = @ + 1]
           ELSE inbox[i]]
    /\ calls' = [calls EXCEPT ![t] = [phase |-> "reading", ids |-> batch,
          priorityKept |-> PriorityKept(batch)]]
    /\ reclaimFenced' = (\A i \in batch : inbox[i].status = "pending" \/
              (~explicitRecovery /\ inbox[i].leaseUntil <= now))
    /\ UNCHANGED <<now, explicitRecovery, releaseExact, completionFenced, renewalFenced>>

ReturnClaims(t, keep) ==
    /\ calls[t].phase = "reading"
    /\ keep \subseteq calls[t].ids
    /\ calls' = [calls EXCEPT ![t].phase = "returned", ![t].ids = keep]
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

Defer(t, i) ==
    /\ HasClaim(t, i)
    /\ inbox[i].status = "processing" /\ inbox[i].token = t
    /\ inbox' = [inbox EXCEPT ![i].status = "pending", ![i].token = 0, ![i].leaseUntil = 0,
                              ![i].attempts = @ - 1, ![i].availableAt = now + 1]
    /\ UNCHANGED <<now, calls, explicitRecovery, releaseExact, completionFenced,
                   reclaimFenced, renewalFenced>>

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
    \/ CommitAdmission
    \/ \E t \in Tokens, n \in 1..2 : ClaimBatch(t, n)
    \/ \E t \in Tokens, keep \in SUBSET Rows : ReturnClaims(t, keep)
    \/ \E t \in Tokens : ReleaseClaimToken(t)
    \/ \E t \in Tokens, i \in Rows : Defer(t, i)
    \/ \E t \in Tokens, i \in Rows : Complete(t, i)
    \/ \E t \in Tokens, i \in Rows : RenewInbox(t, i)
    \/ Tick

OrderingKeyFIFO == inbox[2].status \in {"processing", "completed"} => inbox[1].status = "completed"
OneProcessingPerPartition ==
    \A i \in Rows : Cardinality({j \in Rows : OrderingKey(j) = OrderingKey(i)
                                              /\ inbox[j].status = "processing"}) <= 1
ReadyHeadsByPriority == \A t \in Tokens : calls[t].priorityKept
ReleaseOnlyUnreturned == releaseExact
CompleteOnlyLiveOwner == completionFenced
ReclaimOnlyExpiredImplicit == reclaimFenced
RenewOnlyCurrentOwner == renewalFenced
ExpiredCurrentOwnerCanRenew ==
    \A t \in Tokens, i \in Rows :
        (HasClaim(t, i) /\ inbox[i].status = "processing" /\ inbox[i].token = t
                        /\ inbox[i].leaseUntil <= now) => ENABLED RenewInbox(t, i)
====
