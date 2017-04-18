package avi

import (
	"strings"

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
	cfg        *config.ExtensionConfig
	client     *client.Client
	aviSession *AviSession
}

func initAviSession(host string, port string, username string, password string, sslVerify string) (*AviSession, error) {
	insecure := false
	sslVerify = strings.ToLower(sslVerify)
	if sslVerify == "no" || sslVerify == "false" {
		insecure = true
	}

	netloc := host + ":" + port // 10.0.1.4:9443 typish
	aviSession := NewAviSession(netloc, username, password, insecure)
	err := aviSession.InitiateSession()
	return aviSession, err
}

func log() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"ext": pluginName,
	})
}

func NewAviLoadBalancer(c *config.ExtensionConfig, cl *client.Client) (*AviLoadBalancer, error) {
	aviSession, err := initAviSession(c.AviControllerAddr,
		c.AviControllerPort,
		c.AviUser,
		c.AviPassword,
		c.SSLServerVerify)

	lb := &AviLoadBalancer{
		cfg:        c,
		client:     cl, // docker client
		aviSession: aviSession,
	}

	return lb, err
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
