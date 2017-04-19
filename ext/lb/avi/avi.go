package avi

import (
	"net"
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

var srvcache map[string]map[string]types.Container

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

	if srvcache == nil {
		srvcache = make(map[string]map[string]types.Container)
	}

	return lb, err
}

func (p *AviLoadBalancer) Name() string {
	return pluginName
}

func (p *AviLoadBalancer) processEvent(add bool, cnt types.Container) bool {
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
