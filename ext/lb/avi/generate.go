package avi

import (
	"sync"

	"github.com/docker/engine-api/types"
	"github.com/ehazlett/interlock/ext"
)

var cache map[string]types.Container
var retain map[string]types.Container
var mutex = &sync.Mutex{}

func (p *AviLoadBalancer) GenerateProxyConfig(containers []types.Container) (interface{}, error) {
	mutex.Lock()
	if cache == nil {
		cache = make(map[string]types.Container)
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
		if p.processEvent(true, cnt) {
			retain[cnt.ID] = cnt
		}
	}

	for _, cnt := range cache {
		p.processEvent(false, cnt)
	}

	cache = retain

	mutex.Unlock()
	return nil, nil
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
