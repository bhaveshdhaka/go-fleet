# Runbook — ROLLBACK (fleet lab)

## State rollback (Tier-0 safe)
1. `./fleet status`                        # confirm current stage
2. demote by journal convention: append `event=rejected` line and restore
   prior stage in ops/state/deployments.yaml VIA ./fleet (never sed).
   Until scripted, escalate to owner — single-operator lab, audit trail first.

## Cluster rollback (VM tier)
    kubectl rollout undo deployment/<name> -n fleet-lab
Followed immediately by: `./fleet doctor` and HANDOVER.md update.
