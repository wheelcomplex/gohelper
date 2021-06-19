module main

replace github.com/wheelcomplex/gohelper => ../../

replace gopkg.in/fsnotify.v1 => github.com/fsnotify/fsnotify v1.4.9

go 1.17

require (
	github.com/mitchellh/go-ps v1.0.0
	github.com/wheelcomplex/gotail v0.0.0-20180514194441-a1dbeea552b7
)

require (
	github.com/hpcloud/tail v1.0.0 // indirect
	golang.org/x/sys v0.0.0-20191005200804-aed5e4c7ecf9 // indirect
	gopkg.in/fsnotify.v1 v1.0.0-00010101000000-000000000000 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)
