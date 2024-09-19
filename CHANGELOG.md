# Changelog

## [3.2.6] - 2024-09-20

### Changed

* easier cluster id retrieval that uses less resources
* meaning of the cluster id timeout is now for http request timeout
* transfer files from ssh copy now have a timestamp added, this should make collisions less problematic
* now retrieving the last configuration value from dremio.log.path in case someone has duplicates we will use the final one

## [3.2.5] - 2024-09-13

### Added

* autodetection of gc log name from the logging parameter, this removes the need to set gc log matching pattern
* enhanced logging during file iteration while searching for logs in the gc logging folder

## [3.2.4] - 2024-09-09

### Added 

* added support for using older kubectl clients since the kubectl cp interface is stable, by checking client version we can safely check if retries are supported and only add them if the are

## [3.2.3] - 2024-09-06

### Added

* --context or -x flag to provide support for passing the kubernetes context into ddc, this works for both the 
kubeAPI as well as the the kubectl based collection
* the current context is detected if none is supplied
* added the context used to the logs
* added the context used to the collection type output
* we now log the URL of the kubernetes REST api server

### Removed

* extra log showing free disk space was confusing so it has been removed

## [3.2.2] - 2024-09-03

### Changed

* added note that the executor can be empty during the UI prompts

### Added

* remove host from executor list if it is present in the coordinator list
* remove a host from the list of nodes if it has been specified twice

## [3.2.1] - 2024-08-26

### Added

* timeout for cluster_id search
* timeout for system table export
* added back -t pat prompt this fits in well with current collection strategies and passing in the pat to standard is not obvious for everyone

### Changed

* calling remote cleanup as soon as a tarball is transferred

### Removed

* removed --dremio-pat-token this flag was hidden but still in use in some cases, this was intended to be removed awhile ago use the --pat-prompt instead 

### Fixed

* logger was never being reset after copy to archive, this is fixed

## [3.2.0] - 2024-08-23

### Changed

* Default for gc logs is now "server*.gc*"
* Now collecting ZooKeeper container logs from the default helm chart
* Now collects the previous container logs as well

## [3.1.2] - 2024-06-17

### Added

* added oidc auth for the k8s api

### Changed

* only run task kill on interrupt

## [3.1.1] - 2024-06-12

### Fixed

* Disabled arm64 linux musl build as it's difficult to succeed at
* warnings were not logging warnings in mode --disable-prompt but logging errors

## [3.1.0] - 2024-06-11

### Changed

* DDC will now work on older versions of Linux as we now compile with libmusl
* enhanced output of cleanup tasks in UI

### Fixed

* Windows installs without kubectl now function correctly
* Cleanup tasks were slower than they needed to be. Large speedup on large clusters.

## [3.0.3] - 2024-06-07

### Fixed

* spelling and logging fixes

## [3.0.2] - 2024-06-07

### Fixed

* security fixes 
* upgrade k8s client to 0.30.1
* semantic version correction for go modules

## [3.0.1] - 2024-06-05

### Fixed

* improved logging

## [3.0.0] - 2024-06-04

### Added

* added a standard+jstack option for the --collects flag

### Changed

* removed ttop and replaced it with `LINES=100 top -H -n <iterations> -p <pid> -d <interval> -bw` 
* will try and use `kubectl` for file transfers and command execution if present, but for cluster discovery and the k8s rights check the api is still used, use the `-d` flag to disable this behavior and just use the Kubernetes api directly.
* unhide most of the hidden flags so that people can use the more advanced features if they want
* in k8s pod tarball transfer now increases with node count (coordinators will have more queries.json size) the base is still 30 minutes but we add a minute for every 3 pods

### Fixed

* CTRL+C cancels all running processes and remote calls also executes any file or folder cleanup that was pending
* ignoring errors when searching for cluster ID as high usage clusters have files vanish during search
* improved timeout error messages
* improved UX when waiting on cleanup tasks to finish, CLEANUP TASKS is now a reported status

## [2.4.3] - 2024-04-25

### Changed

* removing sys.boot and sys.cache.objects from health check capture
* using sys.jobs_recent when available instead of queries.json
* sjk jar updated

## [2.4.2] - 2024-04-23

### Added

* support for retrieving GC logs by the jdk-11 compatible flags

## [2.4.1] - 2024-04-12

### Added

* more logging around transfer life cycle
* extra AWSE engine is exported by WLM process

### Changed

* timeout for transfers dropped to 30 minutes from an hour
* dropped retries from 200 to 100 for network transfer

## [2.4.0] - 2024-04-04

### Added

* we now support passing the PAT via stdin

### Changed

* when using DDC standard collection we now pass the pat to local-collect on nodes via standard in using Kubernetes and ssh. This improves security
* bumped retries up to 200 for Kubernetes and dropped the pause time between retries from 100 milliseconds to 50 milliseconds


## [2.3.0] - 2024-03-28

### Changed

* the default job profiles is now 20 for everything except the health check where it remains 25000. This default only works if
someone has added the PAT which is always available
* moved all rest API calls to the start of the process. This is to minimize the amount of operations that fails if a token expires.

## [2.3.0-rc3] - 2024-03-26

### Fixed

* was using move instead of copy for fallback strategy

## [2.3.0-rc2] - 2024-03-21

### Added

* added a --label-selector flag for the kubernetes pods which follows the standard [kubernetes approach](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors), this is entirely optional and will use the current default if nothing is specified
* show the collection arguments used for DDC

### Changed

* queries.json capture days defaults to 30 for --collect standard and --collect health-check
* retrying up to 99 times on the incremental copy of the tarballs
* change the order of configuration values visible in the UI for readability

### Fixed

* prompt UI now is not engaged if --namespace, --disable-prompt, --detect-namespace or --ssh-user is used

## [2.3.0-rc1] - 2024-03-21

### Fixed

* fixed races in retrieval of tarballs from pods
* fixed more output for batch mode

## [2.3.0-beta3] - 2024-03-21

### Added

* retry logic for copy from 
* high hard-coded timeouts for copy from and copy to pod, if someone really wants this to be configurable we accept PRs

## [2.3.0-beta2] - 2024-03-20

### Fixed

* logging had extra whitespaces in it and this broke the output for the UX, cleaning this out now
* logging only individual lines instead of groups of lines together as was possible with kubernetes output, now logging output for k8s should be more logical

## [2.3.0-beta1] - 2024-03-19

### Added

* fine grained updates of job status
* fallback to local collect only when trying to collect with `--detect-namespace` and `--disable-prompt`

### Changed

* -t flag now means for job profile collection threads and all other collection is now single threaded
* disabled jstack by default
* reduced concurrency in cluster copy operation to simplify logic threading

### Fixed

* never closed the reader for kubernetes.CopyToHost 
* no longer waiting on result of stream copy if it fails in kubernetes.CopyToHost should enhance stability in some networks

## [2.2.1-beta1] - 2024-03-13

### Added

* option to change the tmp limits in gb
* --disable-prompt now outputs the status and result as json

## [2.2.0] - 2024-03-07

### Added

* we now support running ddc in a kubernetes pod assuming the following rights have been giving to the pod

### Changed

* concurrent transfers are a bit more aggressive and will attempt to keep 2 transfers going at all time
* use kubernetes api instead of kubectl

### Removed

* kubectl describe node and kubectl describe pod had to be removed as they required kubectl and not available easily via the API

## [2.1.2] - 2024-02-23

### Added

* --pid flag for local-collect
* --transfer-flag for advanced users of the ddc command to override the hard coded value

## [2.1.1] - 2024-02-23

### Added

* more logging on retrieval of dremio home and log directories
* storing ps output from autodetection in diag tarball under the node-info
* log rate of reading files when searching for cluster ID

### Fixed

* error message when findClusterID failed was confusing
* sanitizing intput from autodetection of dremio home, conf and log directories

## [2.1.0] - 2024-02-15

### Added

* retries=5 added to the kubectl cp command
* limit number of transfers of tarballs to 2 at once to help with system or bandwidth limitations

## [2.0.1] - 2024-02-14

### Added

* validation for the --collect mode.

## [2.0.0] - 2024-02-13 

### Fixed

* a log was leaking to the output when it should have been going to ddc.log
* logging error, jstack collection was called gc log collection
* autodetect in gclogging was not working this has been added back

### Removed

* Removed many of the command line flags as they were little used, ddc.yaml still provides all the options needed
* the -k and --k8s flags are now inferred by the use of the --namespace flag

### Added
* ddc interactive prompt when no options are passed to the command that takes you through a menu driven selection
* Added --collect with the available values of light, standard, health-check default is quick. 
  * `--collect light` has no jfr, jstack, ttop and only 2 days of logs and queries.json
  * `--collect standard` is the old default (jfr, ttop, jstack, 28 days of queries.json and 7 days of logs)
  * `--collect healthcheck` is full + fires the pat prompt which adds job profile collection

### Changed 

* k8s collection now no longer takes -c and -e flags, and only a full cluster capture is supported with k8s

## [1.0.1] - 2024-02-07

### Added
* include `ddc.log` in the final diag tarball

## [1.0.0] - 2024-02-01

### Changed

* reporting back failed autodetect due to permissions
* DDC now requires 40gb of space free wherever the --transfer-dir or --tarball-out-dir is specified. If this is not available the collection will fail

## [0.9.1] - 2024-01-25

### Changed

* now will validate REST api configuration sooner in the local-collect process and stop collection if it is invalid
* UI was not reporting correctly when pat was set with DDC.yaml
* have UI update after token prompt for a better UX

## [0.9.0] - 2024-01-12

### Added
* collect kubectl describe output for pods
* better error message for validation failure

### Changed
* Using presence of queries.json or server.log to validate correct logging directory
* Updated list of available system tables for Software and Cloud
* ddc command no longer uses random output directory in /tmp for copy destination, but instead uses the directory for the tarball output
* ttop no longer outputs to the temp folder
* ddc.yaml tmp-output-dir now defaults to using tarball-out-dir as it's base directory
* tmp-output-dir is now deprecated as it was too hard to configure correctly
* default --transfer-dir is now /tmp/ddc-(TIMESTAMP)
* will fail ddc if we use a --tarball-out-dir or a --transfer-dir that has any entries besides: ddc, ddc.log, ddc.yaml or nodeName.tar.gz

### Fixed
* fixed not actually allowing the DREMIO_LOG_DIR to be used 

## [0.8.3] - 2023-12-21

### Fixed
* fixed summary.json missing clusterId and Dremio version

## [0.8.2] - 2023-12-19

### Added
* added more information to the TUI to make clear the collection status
* added addtional info to summary.json

## [0.8.1] - 2023-12-18

### Added
* collect kubectl describe output for nodes
* add `mount` and `lsblk` outputs into `node-info.txt` file for each node

### Fixed
* fixed jcmd command syntax when getting dremio version from process info 

## [0.8.0] - 2023-12-01

### Added
* ddc.log file location on command start and on command end
* ddc falls back to logging to the temp directory if the default location is not present
* --ddc-yaml flag now for local-collect to be able to read a ddc yaml from anywhere
* added validation and logging of ddc.yaml when running the ddc command
* automatic detection of dremio log and configuration directories from the dremio process.
* an effort is made to detect the rocksdb folder
* cluster ID is captured from the rocksdb folder
* dremio version is now captured from the classpath

### Changed
* Major UX redesign: minimized the output from the tool to the essential, no more wall of text
* failing validiation of the dremio log directory
* will fail the collect if log directory only has one file or is inaccessible
* will fail the collect if conf directory is empty or inaccessible
* no longer require executor be given to the ddc command, but we still require a coordinator

### Fixed
* no longer logging we stopped an existing JFR recording when we didn't

## [0.7.4] - 2023-11-22
### Fixed

* AWSE coordinator/executor detection changed - now checks for coordinator first
* JFR now attempts a silent stop for existing DDC JFR recordings, this will clean up any collections that had not been properly stopped

## [0.7.3] - 2023-11-09
### Fixed

* now copy, archive, and delete copy instead of archive in place log files #130
* unexpected use of tmp path #123
* clean rest API URL by adding check for trailing slash #127 

## [0.7.2] - 2023-10-06

### Fixed

* Check PID doesn't return 0 and fail gracefully if it does
* Fixed collections for scale out coordinator(s) for kubernetes
* Fixed issue where PAT token was not being masked in the `ddc.log`

### Changed

* Changed K8s container log collection to be multi-threaded 

## [0.7.1] - 2023-09-01

### Fixed

* SSH banner was breaking file naming for scp transfers from nodes

### Changed

* hostname detection changed to use file under /proc instead of command

## [0.7.0] - 2023-08-02

### Added

* end to end testing around ssh collection

### Fixed 

* fixed ttop collection on premise
* NOTICE file is now present which includes dependencies authors and their copyrights

### Changed

* removed metrics collection due to licensing issues
* removed viper configuration parsing due to licensing issues
* Invalid or missing ddc.yaml will now stop execution
* earlier exit of remote execution of ddc if copying of ddc or ddc.yaml fails

## [0.6.1] - 2023-07-17

### Added

* binary for arm64 windows

### Fixed

* Windows collection and the ability to run tests on Windows

## [0.6.0] - 2023-07-13

### Added

* flag to specify the Dremio pid
* flag to control collection of jvm flags
* flag to control os information collection
* flag to pick up ddc.yaml from a specified directory when running the remote collecting `ddc` command
* flag to disable rest api calls
* ddc awselogs - automates log collection for awse instead of requiring remote connections one can run just this one command from any node where the /var/efs/logs directory is located and get all logs for each node
* we now store the output of kubectl log from each container on all pods that are matched
* added --transfer-flag to change the output directory for ddc and tarball captures. This should allow dealing with limits in partition size to be dealt with

### Changed

* development for ddc now requires a kubernetes cluster
* when used with the main ddc command --dremio-pat-prompt one will receive a password prompt. This password will be sent to all nodes. This removes the need for adding --dremio-pat-token to the yaml
* when using `ddc local-collect --dremio-pat-token ""` the user will receive password prompt
* executors now have --disable-rest-api passed to them instead of a blank --dremio-pat-token
* we now only look for ddc.yaml before we considered a bunch of different configuration file types, this will allow us to simplify out configuration code at a later date
* with the new ability to specify where the transfer files are stored, we no longer brute force delete the transfer folder (which was /tmp/ddc and therefore very safe to delete), we now only delete ddc, ddc.yaml and the tarball that matches the host name from the remote node, this will prevent people from deleting all of their data directory for example)

### Fixed

* when --dremio-pat-token was used it would show up the argument in some logs, this has been corrected
* job profile collection was reporting collected profiles even when they errored out
* dremio-env now has the correct has the correct name of dremio-env
* parsing was silently failed when ddc.yaml had an incorrect format or syntax error, this has been resolved.

## [0.5.0] - 2023-06-21

### Added

* added "ttop" reporting for local-collect which allows reporting on the threads in java by cpu usage and gc allocation rate
* now have dremio-cloud support 

### Fixed

* fixed broken thread pool that was not limiting work at all, now have test to detect this now
* windows development now functions
* added some type assertions around the handling of maps to prevent bugs

### Changed

* if table is missing from system table capture now it will just skip it, this means better handling of different versions.
* less chatty logs and we now have simplified logging of progress in ddc

## [0.4.0] - 2023-06-16

### Added

* configuration options using --rest-http-timeout for setting request timeout during rest collection
* DDC Linux is now embedded in the Windows and Mac versions of DDC, this means one less binary to move around

### Fixed

* gc logs now respect log age (default 7 days)
* windows collection was broken with ddc, this is now fixed
* executor capture was broken on ssh due to difference in argument parsing between k8s and ssh
* sudo now is respected when copying files back and forth with ssh installs

### Changed

* removed ginkgo for testing, this should make contributions easier
* logs are much less chatty in the console, they are still busy in the logs
* default ddc.yaml is much less noisy and busy
* updated code samples in cli

## [0.3.2] - 2023-06-08

### Added

* redaction of PAT token in logs
* enhanced logging of configuration file to help in identifying typos in the conf
* more tests

### Fixed

* REST api capture was disabled, this is now fixed
* job profiles were always selecting one type, now the types of job profiles are spread out
* we were always downloading the same job profile now we are not
* fixed race in cli output to non thread safe implementations
* spelling fixes
* AWSE sometimes is tricky and fools DDC which is the correct PID we have no added more thorough detection

## [0.3.1] - 2023-06-07

### Added

* support for untrusted certs when accessing the dremio API

### Fixed

* We were logging WARN messages for unsupported kinds, we are ignoring those now and there will be a debug log that explains we are not masking those kinds

### Changed

* if unable to detect any dremio PID process we will now exit with an error that states one must run sudo to run collections
* several warnings have the text error in them, this was leading to people thinking there was an error when really it is just a warning that may or may not be significant
* REST calls now only occur on the first coordinator and will not occur on subsequent nodes

## [0.3.0] - 2023-06-02

### Added

* logs now stream from the nodes capturing this give a sense of progress
* added k8s json file password masking
* password masking for dremio.conf
* prevent collection when invalid k8s or ssh is given

### Fixed

* if no gc log is set or found the gc log collection skips
* gclog detection was running on the root ddc command, this has been
  moved to a later evaluation time than the init() command
* was removing the test tmp dir by accident
* fixed a race in the cli that lead to it exiting when streaming before all data had been written out

### Changed

* jps -v is now used to parse startup flags, this works more
  consistently than ps -f
* gclog detection now will log a warn if it overrides a setting
* default gclog directory is now empty in ddc.yaml this should usually
  not need to be set
* matcher on gc logs now will match .current files
* beta enhanced AWSE detection, may still have to manually collect with local-collect in some cases

## [0.3.0-rc1] - 2023-06-01

### Fixed

* awse log directory fix
* awse pid fix
* removed stale logging in multiple packages
* removed no longer used flags for root command
* metrics fix and logger test fix
* cover and test scripts are separate so failures are not buried

### Changed

* updated docs for 0.3.0
* now log both kinds of metrics
* logger name change for warn resulted in failing tests
* tab layout for metrics report

## [0.3.0-beta5] - 2023-06-01

### Fixed

* forgot to call start on thread pool stopping all collection

### Changed

* simplified thread pool
* metrics now collect to json fixing #87 no need to have a flag for now

## [0.3.0-beta4] - 2023-05-31

### Changed

* job profiles now threads
* new improved thread pool that executes more consistently instead of in bunches
* support for more than just gzipped archive logs

### Fixed

* archiving of logs now works correctly and will grab several days of logs

## [0.3.0-beta3] - 2023-05-26

### Added
* added logging for configuration as it has been picked up

### Fixed
* jstack was not pausing long enough between iterations
* don't delete jfr after capturing it. fixes #100
* removed old $ variables fixes #102
* verbosity was broken, now works even if someone asks for more verbosity than we have
* defaults were not set for WLM, export system tables and kvm store report
* inverted check for wlm, kv store, dremio config, disk usage and job profile collections fixes #104 and #98
* command line flags are only bound to viper if they are passed
* command line flag variables are now parsed directly from viper, this allows configuration to override the flags fixed #103
* if archive folder is not available just log an error and skip it
* cli flags take precedence

## [0.3.0-beta2] - 2023-05-25

### Changed

* will no longer collect job profiles when queries.json is not present
  on a node and will log a warning when job collection is enabled.

### Fixed

* fixes https://github.com/dremio/dremio-diagnostic-collector/issues/80 now releases that are not linux contain an extra binary and example ddc.yaml file editing the file will change the parameters that run remotely on the server
* fixed  https://github.com/dremio/dremio-diagnostic-collector/issues/86 local capture metrics were not working
* old thread pool was buggy, wrote new one by hand, works well
* removed a lot of code around capture and collection as it is no longer needed
* logging to two sources, and now includes logs in the final tarball
* local-collect will archive what is had now fixed https://github.com/dremio/dremio-diagnostic-collector/issues/84
* fixes https://github.com/dremio/dremio-diagnostic-collector/issues/77 removing k8s from local-collect
* fixes https://github.com/dremio/dremio-diagnostic-collector/issues/78 consent formatting
* fixes https://github.com/dremio/dremio-diagnostic-collector/issues/79 consent was backwards now works as expected

## [0.2.3] - 2023-05-23

### Fixed
* logs duplicated under incorrect path

## [0.3.0-beta1] - 2023-05-16

### Added

- Local capture command that can run locally on a node. 
- Added configuration file support ddc-capture.yaml in local folder (also supports json, toml, hcl, env, and props file formats). The configuration options are the same name as the flags. Run ddc local-capture --help for more information.

### Changed

- removed all formats except the health check format

## [0.2.2] - 2023-05-15

### Added
- support for sudo


## [0.2.1] - 2023-02-14

### Added

- Easier to use http client

### Fixed

-  Excluding files did not accept wildcards
-  Fix k8s namespace syntax. Use `-n` option instead of `namespace:label`
-  AWSE deployments can have bundle multiple dupe named files under one IP

## [0.2.0] - 2022-12-08

### Added

- Support for health check format
- Support for basic format and other future formats
- Kubernetes cluster level config collection

### Fixed
- Issues with windows path formatting

## [0.1.5] - 2022-11-10

### Added

- Support for JFR collection
- Ability to exclude files (exact match)
- Flag to specify GC log location

### Fixed

- Collected file timestamps being reset to 1970 dates

## [0.1.4] - 2022-10-26

### Added

- testing for windows environments

### Fixed

- newer version of golang ci
- moved errors to defer calls
- running ddc from windows sent the wrong path separator to pods
- running ddc from windows causes syntax issues with drive letter notation


## [0.1.3] - 2022-10-14

### Added

-  limit log collection by age #10

### Fixed

- block wildcard searches #33
- warn and continue when archiving paths that are nil (i.e. the "..data" paths you get in Dremio k8s) #34

## [0.1.2] - 2022-07-18

### Added

- dynamically find the gc.log #19
- recursively search all log and conf directories #27

### Fixed

- when archiving directory we hit an error #24

## [0.1.1] - 2022-07-18

### Added

- added dynamic container names with two new flags to use custom ones --coordinator-container and --executors-container

### Fixed

- the coordinator would never collect it's logs in kubernetes mode as the container name was incorrect

## [0.1.0] - 2022-07-18

### Added

- able to capture logs, configuration and diagnostic data from Dremio clusters deployed on Kubernetes and on-prem
 
[3.2.6]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.2.5...v3.2.6
[3.2.5]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.2.4...v3.2.5
[3.2.4]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.2.3...v3.2.4
[3.2.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.2.2...v3.2.3
[3.2.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.2.1...v3.2.2
[3.2.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.2.0...v3.2.1
[3.2.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.1.2...v3.2.0
[3.1.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.1.1...v3.1.2
[3.1.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.1.0...v3.1.1
[3.1.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.0.3...v3.1.0
[3.0.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.0.2...v3.0.3
[3.0.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.0.1...v3.0.2
[3.0.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v3.0.0...v3.0.1
[3.0.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.4.3...v3.0.0
[2.4.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.4.2...v2.4.3
[2.4.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.4.1...v2.4.2
[2.4.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.4.0...v2.4.1
[2.4.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0...v2.4.0
[2.3.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0-rc3...v2.3.0
[2.3.0-rc3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0-rc2...v2.3.0-rc3
[2.3.0-rc2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0-rc1...v2.3.0-rc2
[2.3.0-rc1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0-beta3...v2.3.0-rc1
[2.3.0-beta3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0-beta2...v2.3.0-beta3
[2.3.0-beta2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.3.0-beta1...v2.3.0-beta2
[2.3.0-beta1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.2.1-beta1...v2.3.0-beta1
[2.2.1-beta1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.2.0...v2.2.1-beta1
[2.2.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.1.2...v2.2.0
[2.1.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.1.1...v2.1.2
[2.1.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.1.0...v2.1.1
[2.1.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.0.2...v2.1.0
[2.0.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.0.1...v2.0.2
[2.0.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v1.0.1...v2.0.0
[1.0.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.9.1...v1.0.0
[0.9.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.8.3...v0.9.0
[0.8.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.8.2...v0.8.3
[0.8.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.4...v0.8.0
[0.7.4]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.6.2...v0.7.0
[0.6.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0-rc1...v0.3.0
[0.3.0-rc1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0-beta5...v0.3.0-rc1
[0.3.0-beta5]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0-beta4...v0.3.0-beta5
[0.3.0-beta4]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0-beta3...v0.3.0-beta4
[0.3.0-beta3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0-beta2...v0.3.0-beta3
[0.3.0-beta2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.3.0-beta1...v0.3.0-beta2
[0.3.0-beta1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.2.2...v0.3.0-beta1
[0.2.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/dremio/dremio-diagnostic-collector/releases/tag/v0.1.0
