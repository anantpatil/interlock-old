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

type VS struct {
	name           string
	poolName       string
	appProfileType string
	sslEnabled     bool
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
	 "network_ref": "%s",
         "pool_ref":"%s",`

func formVSName(serviceName string) string {
	return serviceName + "-docker-ucp"
}

func formPoolName(serviceName string, tasks dockerTasks) string {
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

func getAppProfileType(tasks dockerTasks) (string, bool) {
	// if one of the exposed ports is for 443, then use https
	// profile, if 80, then http other wise tcp
	appProfile := APP_PROFILE_TCP
	sslEnabled := false
	for _, dt := range tasks {
		if dt.privatePort == 443 {
			appProfile = APP_PROFILE_HTTPS
			sslEnabled = true
			break
		}
		if dt.privatePort == 80 {
			appProfile = APP_PROFILE_HTTP
		}
	}

	return appProfile, sslEnabled
}

func CheckVS(serviceName string) (*VS, bool) {
	lock.Lock()
	defer lock.Unlock()

	if vsCache == nil {
		return nil, false
	}

	vsName := formVSName(serviceName)
	vs, ok := vsCache[vsName]
	return vs, ok
}

// create a new VS if doesn't exists in cache
func NewVS(serviceName string, tasks dockerTasks) (*VS, bool) {
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
		// create VS with empty pool
		appProfileType, sslEnabled := getAppProfileType(tasks)
		vs := &VS{vsName, poolName, appProfileType, sslEnabled}
		vsCache[vsName] = vs
		return vs, false
	}
}

// checks if pool exists: returns the pool, else some error
func (lb *AviLoadBalancer) CheckPoolExists(poolName string) (bool, map[string]interface{}, error) {
	var resp map[string]interface{}

	cresp, err := lb.aviSession.GetCollection("/api/pool?name=" + poolName)
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

func (lb *AviLoadBalancer) EnsurePoolExists(poolName string) (map[string]interface{}, error) {
	exists, resp, err := lb.CheckPoolExists(poolName)
	if exists {
		log().Infof("Pool %s already exists", poolName)
	}

	if exists || err != nil {
		return resp, err
	}

	return lb.CreatePool(poolName)
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

func (lb *AviLoadBalancer) RemovePoolMembers(pool map[string]interface{}, deletedTasks dockerTasks) error {
	poolName := pool["name"].(string)
	currMembers := getPoolMembers(pool)
	retained := make([]interface{}, 0)
	for _, server := range currMembers {
		ip := server["ip"].(map[string]interface{})
		ipAddr := ip["addr"].(string)
		port := strconv.FormatInt(int64(server["port"].(float64)), 10)
		key := makeKey(ipAddr, port)
		if _, ok := deletedTasks[key]; ok {
			// this is deleted
			log().Debugf("Deleting pool member with key %s", key)
		} else {
			retained = append(retained, server)
		}
	}

	if len(currMembers) == len(retained) {
		log().Infof("Given members don't exist in pool %s; nothing to remove from pool", poolName)
		return nil
	}

	poolUuid := pool["uuid"].(string)
	pool["servers"] = retained
	log().Debugf("pool after assignment: %s", pool)
	res, err := lb.aviSession.Put("/api/pool/"+poolUuid, pool)
	if err != nil {
		log().Infof("Avi update Pool failed: %v", res)
		return err
	}

	return nil
}

func (lb *AviLoadBalancer) AddPoolMembers(pool map[string]interface{}, addedTasks dockerTasks) error {
	// add new server to pool
	poolName := pool["name"].(string)
	poolUuid := pool["uuid"].(string)
	currMembers := getPoolMembers(pool)
	for _, member := range currMembers {
		port := strconv.FormatInt(int64(member["port"].(float64)), 10)
		ip := member["ip"].(map[string]interface{})
		ipAddr := ip["addr"].(string)
		key := makeKey(ipAddr, port)
		if _, ok := addedTasks[key]; ok {
			// already exists; remove
			delete(addedTasks, key)
		}
	}

	if len(addedTasks) == 0 {
		log().Infof("Pool %s has all intended members, no new member to be added.", poolName)
		return nil
	}

	for _, dt := range addedTasks {
		server := make(map[string]interface{})
		ip := make(map[string]interface{})
		ip["type"] = "V4"
		ip["addr"] = dt.ipAddr
		server["ip"] = ip
		server["port"] = dt.publicPort
		currMembers = append(currMembers, server)
		log().Debugf("currMembers in loop: %s", currMembers)
	}

	pool["servers"] = currMembers
	log().Debugf("pool after assignment: %s", pool)
	res, err := lb.aviSession.Put("/api/pool/"+poolUuid, pool)
	if err != nil {
		log().Infof("Avi update Pool failed: %v", res)
		return err
	}

	return nil
}

// deletePool delete the named pool from Avi.
func (lb *AviLoadBalancer) DeletePool(poolName string) error {
	exists, pool, err := lb.CheckPoolExists(poolName)
	if err != nil || !exists {
		log().Infof("pool does not exist or can't obtain!: %v", pool)
		return err
	}
	poolUuid := pool["uuid"].(string)

	res, err := lb.aviSession.Delete("/api/pool/" + poolUuid)
	if err != nil {
		log().Infof("Error deleting pool %s: %v", poolName, res)
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

func (lb *AviLoadBalancer) Create(vs *VS, tasks dockerTasks) error {
	log().Debugf("Creating pool %s for VS %s", vs.poolName, vs.name)
	pool, err := lb.EnsurePoolExists(vs.poolName)
	if err != nil {
		return err
	}

	log().Debugf("Updating pool %s with members", vs.poolName)
	err = lb.AddPoolMembers(pool, tasks)
	if err != nil {
		return err
	}

	if vs.sslEnabled {
		// add certificate
		err := lb.AddCertificate()
		if err != nil {
			return err
		}
	}

	pvs, err := lb.GetVS(vs.name)

	// TODO: Get the certs from Avi; remove following line
	sslCert := make(map[string]interface{})
	// for now, just mock an empty ref
	sslCert["url"] = ""
	// certName := ""

	if err == nil {
		// VS exists, check port etc
		servicePort := int(pvs["services"].([]interface{})[0].(map[string]interface{})["port"].(float64))
		if (vs.sslEnabled && servicePort == 443) ||
			(!vs.sslEnabled && servicePort == 80) {
			log().Infof("VS already exists %s", vs.name)
			return nil
		}

		// return another service exists with same name error
		return ErrDuplicateVS(vs.name)
	}

	appProfile, err := lb.GetResourceByName("applicationprofile", vs.appProfileType)
	if err != nil {
		return err
	}

	// TODO: if you give networ, it asks for subnet. Fix later.
	nwRefUrl := ""
	// if lb.cfg.AviIPAMNetwork != "" {
	// nwRef, err := lb.GetResourceByName("network", lb.cfg.AviIPAMNetwork)
	// if err != nil {
	// return err
	// }
	// nwRefUrl = nwRef["url"].(string)
	//}

	jsonstr := vsJson

	// TODO: For now, no ssl termination. Only enable ssl if port is
	// 443
	// if vs.sslEnabled {
	// jsonstr += `
	// "ssl_key_and_certificate_refs":[
	// "%s"
	// ],`
	// }

	jsonstr += `
         "services": [{"port": %s, "enable_ssl": %s}]
	    }
	}`

	fqdn := vs.name
	if lb.cfg.AviDNSSubdomain != "" {
		fqdn = fqdn + "." + lb.cfg.AviDNSSubdomain
	}

	//TODO: when supporting ssl termination; fix following which is
	// mocked above
	sslCertRef := sslCert["url"]
	if vs.sslEnabled {
		jsonstr = fmt.Sprintf(jsonstr,
			lb.cfg.AviCloudName,
			appProfile["url"], vs.name, fqdn,
			nwRefUrl, pool["url"], sslCertRef, "443", "true")
	} else {
		jsonstr = fmt.Sprintf(jsonstr,
			lb.cfg.AviCloudName,
			appProfile["url"], vs.name, fqdn,
			nwRefUrl, pool["url"], "80", "false")
	}

	var newVS interface{}
	json.Unmarshal([]byte(jsonstr), &newVS)
	log().Debugf("Sending request to create VS %s", vs.name)
	log().Debugf("DATA: %s", jsonstr)
	nres, err := lb.aviSession.Post("api/macro", newVS)
	if err != nil {
		log().Infof("Failed creating VS: %s", vs.name)
		return err
	}

	log().Debugf("Created VS %s, response: %s", vs.name, nres)
	return nil
}

func (lb *AviLoadBalancer) Delete(vs *VS) error {
	pvs, err := lb.GetVS(vs.name)
	if err != nil {
		log().Warnf("Cloudn't retreive VS %s; error: %s", vs.name, err)
		return err
	}

	if pvs == nil {
		return nil
	}

	iresp, err := lb.aviSession.Delete("/api/virtualservice/" + pvs["uuid"].(string))
	if err != nil {
		log().Warnf("Cloudn't delete VS %s; error: %s", vs.name, err)
		return err
	}

	log().Infof("VS delete response %s", iresp)

	// now delete the pool
	err = lb.DeletePool(vs.poolName)
	if err != nil {
		log().Warnf("Cloudn't delete pool %s; error: %s", vs.poolName, err)
		return err
	}

	return nil
}

func (lb *AviLoadBalancer) AddPoolMember(vs *VS, tasks dockerTasks) error {
	exists, pool, err := lb.CheckPoolExists(vs.poolName)
	if err != nil {
		return err
	}
	if !exists {
		log().Warnf("Pool %s doesn't exist", vs.poolName)
		return nil
	}

	return lb.AddPoolMembers(pool, tasks)
}

func (lb *AviLoadBalancer) RemovePoolMember(vs *VS, tasks dockerTasks) error {
	exists, pool, err := lb.CheckPoolExists(vs.poolName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	return lb.RemovePoolMembers(pool, tasks)
}
