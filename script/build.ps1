# script/build: build binary 

Set-Location "$PSScriptRoot\.."

# this is also set in script/release and is a copy paste
$GIT_SHA=@(git rev-parse --short HEAD)
$VERSION=@(git rev-parse --abbrev-ref HEAD)
$LDFLAGS="-X github.com/rsvihladremio/dremio-diagnostic-collector/cmd.GitSha=$GIT_SHA -X github.com/rsvihladremio/dremio-diagnostic-collector/cmd.Version=$VERSION"
go build -ldflags "$LDFLAGS" -o ./bin/ddc
