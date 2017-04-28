package avi

import (
	"net"
	"strconv"
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

// task maps to pool member in Avi
type dockerTask struct {
	serviceName string
	portType    string // tcp/udp
	ipAddr      string // host IP Address hosting container
	publicPort  int    // publicly exposed port
	privatePort int    // publicly exposed port
}

func NewDockerTask(serviceName string, portType string, ipAddr string, publicPort int, privatePort int) *dockerTask {
	return &dockerTask{serviceName, portType, ipAddr, publicPort, privatePort}
}

func makeKey(ipAddr string, port string) string {
	sep := "-"
	return strings.Join([]string{ipAddr, port}, sep)
}

func (dt *dockerTask) Key() string {
	return makeKey(dt.ipAddr, strconv.Itoa(dt.publicPort))
}

// taskKey -> dockerTask
type dockerTasks map[string]*dockerTask

func NewDockerTasks() dockerTasks {
	return make(dockerTasks)
}

// serviceName -> (taskKey -> dockerTask)
type tasksCache map[string]dockerTasks

func NewTaskCache() tasksCache {
	return make(map[string]dockerTasks)
}

type currentConfig struct {
	services     map[string]bool // services added or deleted
	tasksAdded   tasksCache      // tasks added
	tasksDeleted tasksCache      // tasks deleted
}

func NewCurrentConfig() *currentConfig {
	services := make(map[string]bool)
	tasksAdded := NewTaskCache()
	tasksDeleted := NewTaskCache()

	return &currentConfig{services, tasksAdded, tasksDeleted}
}

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

func (lb *AviLoadBalancer) addTasks(serviceName string, tasks dockerTasks) {
	vs, isExisting := CheckVS(serviceName)
	if !isExisting {
		log().Warnf("VS doesn't exist for task %s", serviceName)
		return
	}

	for _, task := range tasks {
		log().Infof("ADD new task for service %s with (%s, %s/[%d -> %d])",
			vs.name, task.ipAddr, task.portType, task.privatePort, task.publicPort)
	}

	vs.jobsChan <- vsJob{tasks, true}
}

func (lb *AviLoadBalancer) deleteTasks(serviceName string, tasks dockerTasks) {
	vs, isExisting := CheckVS(serviceName)
	if !isExisting {
		log().Warnf("VS doesn't exist for task %s", serviceName)
		return
	}
	for _, task := range tasks {
		log().Infof("DELETE task for service %s with (%s, %s/[%d -> %d])",
			vs.name, task.ipAddr, task.portType, task.privatePort, task.publicPort)
	}

	vs.jobsChan <- vsJob{tasks, false}
}

func (lb *AviLoadBalancer) CreateNewVS(vs *VS, tasks dockerTasks) {
	log().Infof("CREATING VS: %s", vs.name)
	if err := lb.Create(vs, tasks); err != nil {
		log().Warnf("Failed to create VS %s; error %s", vs.name, err)
		// remove from cache
		delete(vsCache, vs.name)
		return
	}

	// check for pool member CRUD operations
	for vsJob := range vs.jobsChan {
		// add pool members
		if vsJob.added {
			if err := lb.AddPoolMember(vs, vsJob.tasks); err != nil {
				log().Warnf("Failed to add pool members to VS %s; error %s", vs.name, err)
			}
		} else {
			if err := lb.RemovePoolMember(vs, vsJob.tasks); err != nil {
				log().Warnf("Failed to remove pool members from VS %s; error %s", vs.name, err)
			}
		}
	}
}

func (lb *AviLoadBalancer) DeleteVS(vs *VS) {
	log().Infof("DELETING VS: %s", vs.name)
	if err := lb.Delete(vs); err != nil {
		log().Warnf("Failed to delete VS %s; error: %s", vs.name, err)
	}
}

func (lb *AviLoadBalancer) addService(serviceName string, tasks dockerTasks) {
	if vs, isExisting := CheckVS(serviceName); isExisting {
		log().Warnf("VS %s already exists", vs.name)
		return
	}
	vsName := formVSName(serviceName)
	vs := NewVS(vsName, serviceName, lb.cfg.AviDNSSubdomain, tasks)
	vsCache[vsName] = vs

	go lb.CreateNewVS(vs, tasks)
}

func (lb *AviLoadBalancer) deleteService(serviceName string) {
	if vs, isExisting := CheckVS(serviceName); isExisting {
		delete(vsCache, vs.name)
		close(vs.jobsChan)
		go lb.DeleteVS(vs)
	} else {
		log().Infof("Error deleting VS; VS for %s doesn't exist.", serviceName)
	}
}

func (lb *AviLoadBalancer) processEvent(add bool, cnt types.Container, cc *currentConfig) bool {
	serviceName := hostname(cnt)
	retain := false

	for _, p := range cnt.Ports {
		if p.PublicPort == 0 || net.ParseIP(p.IP).IsUnspecified() {
			continue
		}
		retain = true
		dt := NewDockerTask(serviceName, p.Type, p.IP, p.PublicPort, p.PrivatePort)
		if add {
			// task/cnt was added; check if new service
			if _, ok := srvcache[serviceName]; !ok {
				srvcache[serviceName] = make(map[string]types.Container)
				// mark service as added
				cc.services[serviceName] = true
			}
			if _, ok := cc.tasksAdded[serviceName]; !ok {
				cc.tasksAdded[serviceName] = NewDockerTasks()
			}
			cc.tasksAdded[serviceName][dt.Key()] = dt
			srvcache[serviceName][cnt.ID] = cnt
		} else {
			// task/cnt was deleted
			if _, ok := cc.tasksDeleted[serviceName]; !ok {
				cc.tasksDeleted[serviceName] = NewDockerTasks()
			}
			cc.tasksDeleted[serviceName][dt.Key()] = dt
			delete(srvcache[serviceName], cnt.ID)
		}
	}
	if !add {
		// task/cnt was deleted; check if service deleted
		if _, ok := srvcache[serviceName]; ok {
			if len(srvcache[serviceName]) == 0 {
				// mark the service as deleted
				cc.services[serviceName] = false
				delete(srvcache, serviceName)
			}
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
