# FAQ

## DDC is resource intensive

do the following in the ddc.yaml:

* set `number-threads: 1`
* set `dremio-jstack-freq-seconds: 10`
* set `dremio-queries-json-num-days: 7`
* if using `--dremio-pat-prompt` when running ddc or setting `dremio-pat-token` in the ddc.yaml then set `number-job-profiles: 50`

## DDC tarball is too big

do the following in the ddc.yaml:

* set `dremio-queries-json-num-days: 7`
* set `collect-gc-logs: false`

## DDC is too slow

do the following in the ddc.yaml:

* set `number-threads: 4`
* if using `--dremio-pat-prompt` when running ddc or setting `dremio-pat-token` in the ddc.yaml then set `number-job-profiles: 50`

## I have a tiny /tmp folder and DDC is filling it up

* set `--transfer-dir` at the cli or if doing a local-collect use `--tarball-out-dir` or set `tarball-out-dir` in ddc.yaml this will avoid the use the /tmp folder (as of ddc 0.9.0)

## DDC didn't capture what I wanted

* read the `ddc-HOSTNAME.log` logs and see what errors there are (ie literally grep for ERROR)
* are the dremio-log-dir, dremio-conf-dir set correctly? (assuming the node is offline or the version of DDC is under 0.8 this may be necessary to set)
* the job profiles, KV report, WLM report, and system table report all need `dremio-pat-token` to be set in ddc.yaml or `--dremio-pat-prompt` to be passed at the command line
* are you running the latest version of DDC? We had over 15 releases  in 2023 containing bug fixes and new functionality, check here https://github.com/dremio/dremio-diagnostic-collector/releases
* If you are running ssh..did you remember to use --sudo-user as the dremio user or as a user with admin rights?

## What is captured by DDC?

### By default

* Perf metrics (cpu and GC usage by thread)
* System disk usage
* Java Flight Recorder recording of 60 seconds
* Jstack thread dump every second for approximately 60 seconds
* server.log and 7 days of archives
* metadata\_refresh.log and 7 days of archives
* reflection.log and 7 days of archives
* queries.json and up to 28 days of archives 
* all dremio configurations
* All gc logs if present

### Optionally with the appropriate change to ddc.yaml

* access.log and 7 days of archives
* audit.log and 7 days of archives
* java heap dump

### Optionally with a Dremio Personal Access Token

* a sampling of job profiles (note 25000 jobs can take 15 minutes to collect)
* dremio key value store report
* dremio work load manager details
* system tables and their details