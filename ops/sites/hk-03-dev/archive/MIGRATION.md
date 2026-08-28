# MIGRATION MANIFEST — site hk-03-dev
migrated_at: 2026-08-28T01:14:56Z
source: /home/openchamber/workspaces/sos-lab
source_engine: sos-lab
source_git_sha: f64d7525494dd7b5519d073f0bfc78b285c996b1
imported:
  config/registry.yaml: byte-preserved
  state/deployed.json: 6 entries
  state/builds.json: 3 entries
  templates/: file-for-file
archive:
  deployed.json: state snapshot as of migration
  builds.json: build history as of migration
secrets: NOT copied — referenced via secrets_dir (gitignored at source)
authoritative_engine: fleet (cutover at migration)
