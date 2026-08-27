# Runbook — DEPLOY (fleet lab)
# Audience: humans AND agents. Agent rule: prefer ./fleet over manual steps.

## Rehearsal tier (no cluster needed)
    ./fleet promote <component> stage      # enforces gates, updates state
    kubectl apply --dry-run=client -f infra/k8s/

## Cluster tier (needs the k3s host)
    ./fleet promote <component> prod       # gates include live health probes
    source blocks 04                       # or scripts/blocks/04-deploy.sh usage

## After any deploy
    ./fleet doctor                         # MUST be ALL CLEAR before handoff
