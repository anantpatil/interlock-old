package avi

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	etypes "github.com/docker/engine-api/types/events"
	"github.com/ehazlett/interlock/config"
)

const (
	pluginName = "avi"
)

type AviLoadBalancer struct {
	cfg    *config.ExtensionConfig
	client *client.Client
}

func log() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"ext": pluginName,
	})
}

func NewAviLoadBalancer(c *config.ExtensionConfig, cl *client.Client) (*AviLoadBalancer, error) {
	lb := &AviLoadBalancer{
		cfg:    c,
		client: cl,
	}

	return lb, nil
}

func (p *AviLoadBalancer) Name() string {
	return pluginName
}

func (p *AviLoadBalancer) HandleEvent(event *etypes.Message) error {
	return nil
}

func (p *AviLoadBalancer) ConfigPath() string {
	return ""
}

func (p *AviLoadBalancer) Template() string {
	return ""
}

func (p *AviLoadBalancer) NeedsReload() bool {
	return false
}

func (p *AviLoadBalancer) Reload(proxyContainers []types.Container) error {
	return nil
}
