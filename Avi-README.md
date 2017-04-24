# Interlock [![Build Status](https://travis-ci.org/ehazlett/interlock.svg?branch=master)](https://travis-ci.org/ehazlett/interlock)
Dynamic, event-driven extension system using [Swarm](https://github.com/docker/swarm).  Extensions include HAProxy and Nginx for dynamic load balancing.

The recommended release is `ehazlett/interlock:1.3.0`

# Quickstart
For a quick start with Compose, see the [Swarm Example](docs/examples/nginx-swarm-machine).

# Documentation
To get started with Interlock view the [Documentation](docs).

# Building
To build a local copy of Interlock, you must have the following:

- Go 1.5+
- Use the Go vendor experiment

You can use the `Makefile` to build the binary.  For example:

`make build`

This will build the binary in `cmd/interlock/interlock`.

There is also a Docker image target in the makefile.  You can build it with
`make image`.

# License
Licensed under the Apache License, Version 2.0. See LICENSE for full license text.

# TODO
- [ ] CRUD VS in Avi when service CRUD operation is received
    1. Fill in port from UCP service; if port == 443, enable SSL
    2. If port == 80 or 443, use HTTP application profile, else L4 application profile
    3. Set auto_allocate_vip
    4. fqdn should be appname.subdomain 
    5. Create PoolGroup and a Pool. PoolGroup/Pool name should be appname-pool[group]-containerport-protocol. Fill in PoolGroup/pool in VS
- [ ] CRUD Pool in Avi when service task CRUD operation is received
