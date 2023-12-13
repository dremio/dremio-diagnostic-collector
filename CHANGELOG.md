# Changelog
## [0.8.0]

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

## [0.7.4]
### Fixed

* AWSE coordinator/executor detection changed - now checks for coordinator first
* JFR now attempts a silent stop for existing DDC JFR recordings, this will clean up any collections that had not been properly stopped

## [0.7.3]
### Fixed

* now copy, archive, and delete copy instead of archive in place log files #130
* unexpected use of tmp path #123
* clean rest API URL by adding check for trailing slash #127 

## [0.7.2]

### Fixed

* Check PID doesn't return 0 and fail gracefully if it does
* Fixed collections for scale out coordinator(s) for kubernetes
* Fixed issue where PAT token was not being masked in the `ddc.log`

### Changed

* Changed K8s container log collection to be multi-threaded 

## [0.7.1]

### Fixed

* SSH banner was breaking file naming for scp transfers from nodes

### Changed

* hostname detection changed to use file under /proc instead of command

## [0.7.0]

### Fixed 

* fixed ttop collection on premise
* NOTICE file is now present which includes dependencies authors and their copyrights

### Changed

* removed metrics collection due to licensing issues
* removed viper configuration parsing due to licensing issues

## [0.6.2]

### Added

* end to end testing around ssh collection

### Changed

* Invalid or missing ddc.yaml will now stop execution
* earlier exit of remote execution of ddc if copying of ddc or ddc.yaml fails

## [0.6.1]

### Added

* binary for arm64 windows

### Fixed

* Windows collection and the ability to run tests on Windows

## [0.6.0]

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

## [0.5.0]

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

## [0.4.0]

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

## [0.3.2]

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

## [0.3.1]

### Added

* support for untrusted certs when accessing the dremio API

### Fixed

* We were logging WARN messages for unsupported kinds, we are ignoring those now and there will be a debug log that explains we are not masking those kinds

### Changed

* if unable to detect any dremio PID process we will now exit with an error that states one must run sudo to run collections
* several warnings have the text error in them, this was leading to people thinking there was an error when really it is just a warning that may or may not be significant
* REST calls now only occur on the first coordinator and will not occur on subsequent nodes

## [0.3.0]

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

## [0.3.0-rc1]

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

## [0.3.0-beta5]

### Fixed

* forgot to call start on thread pool stopping all collection

### Changed

* simplified thread pool
* metrics now collect to json fixing #87 no need to have a flag for now

## [0.3.0-beta4]

### Changed

* job profiles now threads
* new improved thread pool that executes more consistently instead of in bunches
* support for more than just gzipped archive logs

### Fixed

* archiving of logs now works correctly and will grab several days of logs

## [0.3.0-beta3]

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

## [0.3.0-beta2]

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


## [0.3.0-beta1] - 2023-05-15

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

- able to capture logs, configuration and diagnostic data from dremio clusters deployed on Kubernetes and on-prem

[0.8.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.4...v0.8.0
[0.7.4]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.6.2...v0.7.0
[0.6.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.6.1...v0.6.2
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
[0.2.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/dremio/dremio-diagnostic-collector/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/dremio/dremio-diagnostic-collector/releases/tag/v0.1.0
