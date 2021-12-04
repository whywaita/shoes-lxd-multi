module github.com/whywaita/shoes-lxd-multi/server

go 1.16

require (
	github.com/docker/go-units v0.4.0
	github.com/lxc/lxd v0.0.0-20211202222358-a293da71aeb0
	github.com/prometheus/client_golang v1.11.0
	github.com/whywaita/myshoes v1.10.4
	github.com/whywaita/shoes-lxd-multi/proto.go v0.0.0-20211203151606-53728ef694c2
	google.golang.org/grpc v1.42.0
)

//replace github.com/flosch/pongo2 => github.com/flosch/pongo2/v4 v4.0.2
