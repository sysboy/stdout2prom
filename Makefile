stdout2prom:	stdout2prom.go
	CGO_ENABLED=0 go build -a -ldflags '-s' -o stdout2prom
