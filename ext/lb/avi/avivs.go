package avi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	APP_PROFILE_HTTPS = "System-Secure-HTTP"
	APP_PROFILE_HTTP  = "System-HTTP"
	APP_PROFILE_TCP   = "System-L4-Application"
)

type ErrDuplicateVS string

func (val ErrDuplicateVS) Error() string {
	return fmt.Sprintf("VS with name %v already exists", string(val))
}

func (val ErrDuplicateVS) String() string {
	return fmt.Sprintf("ErrDuplicateVS(%v)", string(val))
}

type PoolMember struct {
	ip   string
	port int
}

type Pool struct {
	name    string
	members []PoolMember
}

type VS struct {
	name           string
	appProfileType string
	pool           *Pool
	lb             *AviLoadBalancer
}

var vsCache map[string]*VS
var lock = &sync.Mutex{}

var vsJson = `{
       "uri_path":"/api/virtualservice",
       "model_name":"virtualservice",
       "data":{
         "network_profile_name":"System-TCP-Proxy",
         "flow_dist":"LOAD_AWARE",
         "delay_fairness":false,
         "avi_allocated_vip":false,
         "scaleout_ecmp":false,
         "analytics_profile_name":"System-Analytics-Profile",
         "cloud_type":"CLOUD_NONE",
         "weight":1,
         "cloud_name":"%s",
         "avi_allocated_fip":false,
         "max_cps_per_client":0,
         "type":"VS_TYPE_NORMAL",
         "use_bridge_ip_as_vip":false,
         "ign_pool_net_reach":true,
         "east_west_placement":false,
         "limit_doser":false,
         "ssl_sess_cache_avg_size":1024,
         "enable_autogw":true,
         "auto_allocate_ip":true,
         "enabled":true,
         "analytics_policy":{
           "client_insights":"ACTIVE",
           "metrics_realtime_update":{
             "duration":60,
             "enabled":false},
           "full_client_logs":{
             "duration":30,
             "enabled":false},
           "client_log_filters":[],
           "client_insights_sampling":{}
         },
         "vs_datascripts":[],
         "application_profile_ref":"%s",
	 "auto_allocate_ip": true,
         "name":"%s",
         "fqdn": "%s",
	 "network_ref": %s,
         "pool_ref":"%s",`

func formVSName(serviceName string) string {
	return serviceName + "-docker-ucp"
}

func formPoolName(serviceName string, tasks DockerTasks) string {
	// currently, each public exposed port + host ip is a pool member
	// for now assume only one port is mapped either 443 or 80 or
	// something else
	port := 0
	portType := ""
	for _, dt := range tasks {
		port = dt.privatePort
		portType = dt.portType
		if port == 443 {
			break
		}
	}
	sep := "-"
	tokens := []string{serviceName, "pool", strconv.Itoa(port), portType}
	return strings.Join(tokens, sep)
}

// func formPoolGroupName(serviceName string, portType string, port int) string {
// sep := "-"
// tokens := []string{serviceName, "poolgroup", strconv.Itoa(port), portType}
// return strings.Join(tokens, sep)
// }

func GetVS(serviceName string) (*VS, bool) {
	vsName := formVSName(serviceName)

	lock.Lock()
	defer lock.Unlock()

	vs, ok := vsCache[vsName]
	return vs, ok
}

func getAppProfileType(tasks DockerTasks) string {
	// if one of the exposed ports is for 443, then use https
	// profile, if 80, then http other wise tcp
	appProfile := APP_PROFILE_TCP
	for _, dt := range tasks {
		if dt.privatePort == 443 {
			appProfile = APP_PROFILE_HTTPS
			break
		}
		if dt.privatePort == 80 {
			appProfile = APP_PROFILE_HTTP
		}
	}

	return appProfile
}

// form a new VS given a docker task
func VSFromTask(serviceName string, tasks DockerTasks, lb *AviLoadBalancer) (*VS, bool) {
	// tasks contain publicly exposed port from each container on each
	// host for a service
	// currently, each public exposed port + host ip is a pool member
	lock.Lock()
	defer lock.Unlock()

	vsName := formVSName(serviceName)

	if vsCache == nil {
		vsCache = make(map[string]*VS)
	}

	if vs, ok := vsCache[vsName]; ok {
		return vs, true
	} else {
		// create an empty pool
		poolName := formPoolName(serviceName, tasks)
		pool := &Pool{poolName, []PoolMember{}}
		// create VS with empty pool
		appProfileType := getAppProfileType(tasks)
		vs := &VS{vsName, appProfileType, pool, lb}
		vsCache[vsName] = vs
		return vs, false
	}
}

// checks if pool exists: returns the pool, else some error
func (lb *AviLoadBalancer) CheckPoolExists(poolname string) (bool, map[string]interface{}, error) {
	var resp map[string]interface{}

	cresp, err := lb.aviSession.GetCollection("/api/pool?name=" + poolname)
	if err != nil {
		log().Infof("Avi PoolExists check failed: %v", cresp)
		return false, resp, err
	}

	if cresp.Count == 0 {
		return false, resp, nil
	}
	nres, err := ConvertAviResponseToMapInterface(cresp.Results[0])
	if err != nil {
		return true, resp, err
	}
	return true, nres.(map[string]interface{}), nil
}

func (lb *AviLoadBalancer) GetCloudRef() (string, error) {
	cloudName := lb.cfg.AviCloudName
	if cloudName == "Default-Cloud" {
		return "", nil
	}
	cloud, err := lb.GetResourceByName("cloud", cloudName)
	if err != nil {
		return "", err
	}
	return cloud["url"].(string), nil
}

func (lb *AviLoadBalancer) GetResourceByName(resource, objname string) (map[string]interface{}, error) {
	resp := make(map[string]interface{})
	res, err := lb.aviSession.GetCollection("/api/" + resource + "?name=" + objname)
	if err != nil {
		log().Infof("Avi object exists check (res: %s, name: %s) failed: %v", resource, objname, res)
		return resp, err
	}

	if res.Count == 0 {
		return resp, fmt.Errorf("Resource name %s of type %s does not exist on the Avi Controller",
			objname, resource)
	}
	nres, err := ConvertAviResponseToMapInterface(res.Results[0])
	if err != nil {
		log().Infof("Resource unmarshal failed: %v", string(res.Results[0]))
		return resp, err
	}
	return nres.(map[string]interface{}), nil
}

func (lb *AviLoadBalancer) EnsurePoolExists(poolname string) (map[string]interface{}, error) {
	exists, resp, err := lb.CheckPoolExists(poolname)
	if exists || err != nil {
		return resp, err
	}

	return lb.CreatePool(poolname)
}

func getPoolMembers(pool interface{}) []map[string]interface{} {
	servers := make([]map[string]interface{}, 0)
	pooldict := pool.(map[string]interface{})
	if pooldict["servers"] == nil {
		return servers
	}
	_servers := pooldict["servers"].([]interface{})
	for _, server := range _servers {
		servers = append(servers, server.(map[string]interface{}))
	}

	// defport := int(pooldict["default_server_port"].(float64))
	// for _, server := range servers {
	// if server["port"] == nil {
	// server["port"] = defport
	//}
	// }

	return servers
}

func (lb *AviLoadBalancer) RemovePoolMembers(pool map[string]interface{}, deletedTasks DockerTasks) error {
	currMembers := getPoolMembers(pool)
	retained := make([]interface{}, 0)
	for _, server := range currMembers {
		ip := server["ip"].(map[string]interface{})
		key := makeKey(ip["addr"].(string), server["port"].(string))
		if _, ok := deletedTasks[key]; !ok {
			// this is deleted
			log().Debug("Deleting pool member with key %s", key)
		} else {
			retained = append(retained, server)
		}
	}

	pool_uuid := pool["uuid"].(string)
	pool["servers"] = retained
	log().Debug("pool after assignment: %s", pool)
	res, err := lb.aviSession.Put("/api/pool/"+pool_uuid, pool)
	if err != nil {
		log().Infof("Avi update Pool failed: %v", res)
		return err
	}

	return nil
}

func (lb *AviLoadBalancer) AddPoolMembers(pool map[string]interface{}, addedTasks DockerTasks) error {
	// add new server to pool
	pool_uuid := pool["uuid"].(string)
	currMembers := getPoolMembers(pool)
	for _, dt := range addedTasks {
		server := make(map[string]interface{})
		ip := make(map[string]interface{})
		ip["type"] = "V4"
		ip["addr"] = dt.ipAddr
		server["ip"] = ip
		server["port"] = dt.publicPort
		currMembers = append(currMembers, server)
		log().Debug("currMembers in loop: %s", currMembers)
	}

	pool["servers"] = currMembers
	log().Debug("pool after assignment: %s", pool)
	res, err := lb.aviSession.Put("/api/pool/"+pool_uuid, pool)
	if err != nil {
		log().Infof("Avi update Pool failed: %v", res)
		return err
	}

	return nil
}

// deletePool delete the named pool from Avi.
func (lb *AviLoadBalancer) DeletePool(poolname string) error {
	exists, pool, err := lb.CheckPoolExists(poolname)
	if err != nil || !exists {
		log().Infof("pool does not exist or can't obtain!: %v", pool)
		return err
	}
	pool_uuid := pool["uuid"].(string)

	res, err := lb.aviSession.Delete("/api/pool/" + pool_uuid)
	if err != nil {
		log().Infof("Error deleting pool %s: %v", poolname, res)
		return err
	}

	return nil
}

func (lb *AviLoadBalancer) GetVS(vsname string) (map[string]interface{}, error) {
	resp := make(map[string]interface{})
	res, err := lb.aviSession.GetCollection("/api/virtualservice?name=" + vsname)
	if err != nil {
		log().Infof("Avi VS Exists check failed: %v", res)
		return resp, err
	}

	if res.Count == 0 {
		return resp, fmt.Errorf("Virtual Service %s does not exist on the Avi Controller",
			vsname)
	}
	nres, err := ConvertAviResponseToMapInterface(res.Results[0])
	if err != nil {
		log().Infof("VS unmarshal failed: %v", string(res.Results[0]))
		return resp, err
	}
	return nres.(map[string]interface{}), nil
}

func (lb *AviLoadBalancer) CreatePool(poolName string) (map[string]interface{}, error) {
	var resp map[string]interface{}
	pool := make(map[string]string)
	pool["name"] = poolName
	cref, err := lb.GetCloudRef()
	if err != nil {
		return resp, err
	}
	if cref != "" {
		pool["cloud_ref"] = cref
	}
	pres, err := lb.aviSession.Post("/api/pool", pool)
	if err != nil {
		log().Infof("Error creating pool %s: %v", poolName, pres)
		return resp, err
	}

	return pres.(map[string]interface{}), nil
}

func (lb *AviLoadBalancer) AddCertificate() error {
	return nil
}

func (vs *VS) Create(tasks DockerTasks) error {
	log().Debug("Creating pool %s for VS %s", vs.pool.name, vs.name)
	pool, err := vs.lb.EnsurePoolExists(vs.pool.name)
	if err != nil {
		return err
	}

	log().Debug("Updating pool %s with members", vs.pool.name)
	err = vs.lb.AddPoolMembers(pool, tasks)
	if err != nil {
		return err
	}

	ssl_app := false
	if vs.appProfileType == APP_PROFILE_HTTPS {
		// add certificate
		ssl_app = true
		err := vs.lb.AddCertificate()
		if err != nil {
			return err
		}
	}

	pvs, err := vs.lb.GetVS(vs.name)
	// TODO: Get the certs from Avi; remove following line
	ssl_cert := make(map[string]interface{})
	certName := ""
	if err == nil {
		// VS exists, check port etc
		service_port := int(pvs["services"].([]interface{})[0].(map[string]interface{})["port"].(float64))
		if ssl_app &&
			service_port == 443 &&
			pvs["ssl_key_and_certificate_refs"].([]interface{})[0].(string) == ssl_cert["url"].(string) {
			log().Infof("VS already exists %s", certName)
			return nil
		}
		if !ssl_app && service_port == 80 {
			return nil
		}

		// return another service exists with same name error
		return ErrDuplicateVS(vs.name)
	}

	appProfile, err := vs.lb.GetResourceByName("applicationprofile", vs.appProfileType)
	if err != nil {
		return err
	}

	nwRefUrl := ""
	if vs.lb.cfg.AviIPAMNetwork != "" {
		nwRef, err := vs.lb.GetResourceByName("networkprofile", vs.lb.cfg.AviIPAMNetwork)
		if err != nil {
			return err
		}
		nwRefUrl = nwRef["url"].(string)
	}

	jsonstr := vsJson
	if ssl_app {
		jsonstr += `
         "ssl_key_and_certificate_refs":[
           "%s"
         ],`
	}

	jsonstr += `
         "services": [{"port": %s, "enable_ssl": %s}]
	    }
	}`

	fqdn := vs.name
	if vs.lb.cfg.AviDNSSubdomain != "" {
		fqdn = fqdn + "." + vs.lb.cfg.AviDNSSubdomain
	}

	if ssl_app {
		jsonstr = fmt.Sprintf(jsonstr,
			vs.lb.cfg.AviCloudName,
			appProfile["url"], vs.name, fqdn,
			nwRefUrl, pool["url"], ssl_cert["url"], "443", "true")
	} else {
		jsonstr = fmt.Sprintf(jsonstr,
			vs.lb.cfg.AviCloudName,
			appProfile["url"], vs.name, fqdn,
			nwRefUrl, pool["url"], "80", "false")
	}

	var newVS interface{}
	json.Unmarshal([]byte(jsonstr), &newVS)
	log().Debug("Sending request to create VS %s", vs.name)
	nres, err := vs.lb.aviSession.Post("api/macro", newVS)
	if err != nil {
		log().Info("Failed creating VS: %s", vs.name)
		return err
	}

	log().Debug("Created VS %s, response: %s", vs.name, nres)
	return nil
}

func (vs *VS) Delete() error {
	// assume all members from pool are already deleted
	vs.lb.DeletePool(vs.pool.name)
	pvs, err := vs.lb.GetVS(vs.name)
	if err != nil {
		log().Warn("Cloudn't retreive VS %s; error: %s", vs.name, err)
		return err
	}

	if pvs == nil {
		return nil
	}

	iresp, err := vs.lb.aviSession.Delete("/api/virtualservice/" + pvs["uuid"].(string))
	if err != nil {
		log().Warn("Cloudn't delete VS %s; error: %s", vs.name, err)
		return err
	}

	log().Infof("VS delete response %s", iresp)
	return nil
}

func (vs *VS) AddPoolMember(tasks DockerTasks) error {
	exists, pool, err := vs.lb.CheckPoolExists(vs.pool.name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return vs.lb.AddPoolMembers(pool, tasks)
}

func (vs *VS) RemovePoolMember(tasks DockerTasks) error {
	exists, pool, err := vs.lb.CheckPoolExists(vs.pool.name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return vs.lb.RemovePoolMembers(pool, tasks)
}
