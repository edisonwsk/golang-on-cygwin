This is an experimental port to Cygwin.  Nothing here is stable.

To check out this repo, run the following command:
```

hg clone https://golang-on-cygwin.googlecode.com/hg/ golang-on-cygwin 
```
To build, check out the code and set the following environment variables:
```
export GOROOT=/path/to/golang-on-cygwin
export GOARCH=386
export GOOS=linux
export GOBIN=/path/to/your/local/bin
export PATH=$PATH:$GOBIN
```