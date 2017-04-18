package avi

import (
	"net"
	"sync"

	"github.com/docker/engine-api/types"
	"github.com/ehazlett/interlock/ext"
)

var cache map[string]types.Container
var srvcache map[string]map[string]types.Container
var retain map[string]types.Container
var mutex = &sync.Mutex{}

func (p *AviLoadBalancer) GenerateProxyConfig(containers []types.Container) (interface{}, error) {
	mutex.Lock()
	if cache == nil {
		cache = make(map[string]types.Container)
	}
	if srvcache == nil {
		srvcache = make(map[string]map[string]types.Container)
	}
	retain = make(map[string]types.Container)
	for _, cnt := range containers {
		if _, ok := cache[cnt.ID]; ok {
			retain[cnt.ID] = cnt
			delete(cache, cnt.ID)
			continue
		}

		servicename := hostname(cnt)
		if servicename == "" {
			continue
		}
		if processEvent(true, cnt) {
			retain[cnt.ID] = cnt
		}
	}

	for _, cnt := range cache {
		processEvent(false, cnt)
	}

	cache = retain

	mutex.Unlock()
	return nil, nil
}

func processEvent(add bool, cnt types.Container) bool {
	servicename := hostname(cnt)
	retain := false
	for _, p := range cnt.Ports {
		if p.PublicPort == 0 || net.ParseIP(p.IP).IsUnspecified() {
			continue
		}
		retain = true
		op := "DELETE"
		if add {
			op = "POST"
			if _, ok := srvcache[servicename]; !ok {
				srvcache[servicename] = make(map[string]types.Container)
				// CRUD operation to create a new service
				log().Infof("POST new service :  %s", servicename)
			}
			srvcache[servicename][cnt.ID] = cnt
		}

		// CRUD operation to add or delete a backend for a service
		log().Infof("%s operation on a Task for service %s with (%s, %s/%d)", op, servicename, p.IP, p.Type, p.PublicPort)
	}
	if !add {
		delete(srvcache[servicename], cnt.ID)
		if len(srvcache[servicename]) == 0 {
			// CRUD operation to delete a service
			log().Infof("DELETE service :  %s", servicename)
			delete(srvcache, servicename)
		}
	}
	return retain
}

func hostname(c types.Container) string {
	if v, ok := c.Labels["com.docker.swarm.service.name"]; ok {
		return v
	}

	if v, ok := c.Labels[ext.InterlockHostnameLabel]; ok {
		return v
	}

	return ""
}
