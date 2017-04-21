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

func (lb *AviLoadBalancer) Name() string {
	return pluginName
}

func (lb *AviLoadBalancer) taskAdd(serviceName string, ip string, portType string, publicPort int) error {
	log().Infof("ADD new task for service %s with (%s, %s/%d)", serviceName, ip, portType, publicPort)
	// TODO
	return nil
}

func (lb *AviLoadBalancer) taskDelete(serviceName string, ip string, portType string, publicPort int) error {
	log().Infof("DELETE task for service %s with (%s, %s/%d)", serviceName, ip, portType, publicPort)
	// TODO
	return nil
}

func (lb *AviLoadBalancer) serviceAdd(serviceName string, cnt types.Container) error {
	log().Infof("ADD VS :  %s", serviceName)
	// TODO
	return nil
}

func (lb *AviLoadBalancer) serviceDelete(serviceName string, cnt types.Container) error {
	log().Infof("DELETE VS :  %s", serviceName)
	// TODO
	return nil
}

func (lb *AviLoadBalancer) processEvent(add bool, cnt types.Container) bool {
	serviceName := hostname(cnt)
	retain := false
	for _, p := range cnt.Ports {
		if p.PublicPort == 0 || net.ParseIP(p.IP).IsUnspecified() {
			continue
		}
		retain = true
		if add {
			// task/cnt was added; check if new service
			if _, ok := srvcache[serviceName]; !ok {
				srvcache[serviceName] = make(map[string]types.Container)
				// Create a new service
				lb.serviceAdd(serviceName, cnt)
			}
			lb.taskAdd(serviceName, p.IP, p.Type, p.PublicPort)
			srvcache[serviceName][cnt.ID] = cnt
		} else {
			// task/cnt was deleted
			lb.taskDelete(serviceName, p.IP, p.Type, p.PublicPort)
			delete(srvcache[serviceName], cnt.ID)
		}
	}
	if !add {
		// task/cnt was deleted; check if service deleted
		if len(srvcache[serviceName]) == 0 {
			//Delete the service
			lb.serviceDelete(serviceName, cnt)
			delete(srvcache, serviceName)
		}
	}

	return retain
}

func (lb *AviLoadBalancer) HandleEvent(event *etypes.Message) error {
	return nil
}

func (lb *AviLoadBalancer) ConfigPath() string {
	return ""
}

func (lb *AviLoadBalancer) Template() string {
	return ""
}

func (lb *AviLoadBalancer) NeedsReload() bool {
	return false
}

func (lb *AviLoadBalancer) Reload(proxyContainers []types.Container) error {
	return nil
}
