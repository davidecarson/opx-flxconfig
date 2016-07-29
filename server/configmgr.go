//
//Copyright [2016] [SnapRoute Inc]
//
//Licensed under the Apache License, Version 2.0 (the "License");
//you may not use this file except in compliance with the License.
//You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
//	 Unless required by applicable law or agreed to in writing, software
//	 distributed under the License is distributed on an "AS IS" BASIS,
//	 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	 See the License for the specific language governing permissions and
//	 limitations under the License.
//
// _______  __       __________   ___      _______.____    __    ____  __  .___________.  ______  __    __
// |   ____||  |     |   ____\  \ /  /     /       |\   \  /  \  /   / |  | |           | /      ||  |  |  |
// |  |__   |  |     |  |__   \  V  /     |   (----` \   \/    \/   /  |  | `---|  |----`|  ,----'|  |__|  |
// |   __|  |  |     |   __|   >   <       \   \      \            /   |  |     |  |     |  |     |   __   |
// |  |     |  `----.|  |____ /  .  \  .----)   |      \    /\    /    |  |     |  |     |  `----.|  |  |  |
// |__|     |_______||_______/__/ \__\ |_______/        \__/  \__/     |__|     |__|      \______||__|  |__|
//

package server

import (
	//"asicd/asicdCommonDefs"
	"config/actions"
	"config/apis"
	"config/clients"
	"config/objects"
	"encoding/json"
	"fmt"
	"io/ioutil"
	modelObjs "models/objects"
	"os"
	"os/signal"
	"syscall"
	"time"
	"utils/logging"
)

type ConfigMgr struct {
	logger      *logging.Writer
	paramsDir   string
	dbHdl       *objects.DbHandler
	bringUpTime time.Time
	swVersion   SwVersion
	ApiMgr      *apis.ApiMgr
	clientMgr   *clients.ClientMgr
	objectMgr   *objects.ObjectMgr
	actionMgr   *actions.ActionMgr
	cltNameCh   chan string
}

var gConfigMgr *ConfigMgr

const (
	MAX_COUNT_AUTO_DISCOVER_OBJ int64 = 200
)

var futureObjKey map[string][]int32 // Object Name and key

type ConfdGlobals struct {
	Name  string `json: "Name"`
	Value string `json: "Value"`
}

// Get the http port on which rest api calls will be received
func GetConfigHandlerPort(paramsDir string) (bool, string) {
	var globals []ConfdGlobals
	var port string

	globalsFile := paramsDir + "/globals.json"
	bytes, err := ioutil.ReadFile(globalsFile)
	if err != nil {
		gConfigMgr.logger.Err(fmt.Sprintln("Error in reading globals file", globalsFile))
		return false, port
	}

	err = json.Unmarshal(bytes, &globals)
	if err != nil {
		gConfigMgr.logger.Err("Failed to Unmarshall Json")
		return false, port
	}
	for _, global := range globals {
		if global.Name == "httpport" {
			port = global.Value
			return true, port
		}
	}
	return false, port
}

//
// This function would work as a classical constructor for the
// configMgr object
//
func NewConfigMgr(paramsDir string, logger *logging.Writer) *ConfigMgr {
	mgr := new(ConfigMgr)
	mgr.logger = logger
	mgr.paramsDir = paramsDir

	mgr.dbHdl = objects.InstantiateDbIf(logger)

	paramsFile := paramsDir + "/clients.json"
	mgr.dbHdl = objects.InstantiateDbIf(logger)

	mgr.clientMgr = clients.InitializeClientMgr(paramsFile, logger, GetSystemStatus, GetSystemSwVersion, actions.ExecuteConfigurationAction)

	objects.CreateObjectMap()
	objectConfigFiles := [...]string{paramsDir + "/genObjectConfig.json"}
	mgr.objectMgr = objects.InitializeObjectMgr(objectConfigFiles[:], logger, mgr.clientMgr)

	actions.CreateActionMap()
	actionConfigFiles := [...]string{paramsDir + "/genObjectAction.json"}
	mgr.actionMgr = actions.InitializeActionMgr(paramsDir, actionConfigFiles[:], logger, mgr.dbHdl, mgr.objectMgr, mgr.clientMgr)

	mgr.ApiMgr = apis.InitializeApiMgr(paramsDir, logger, mgr.dbHdl, mgr.clientMgr, mgr.objectMgr, mgr.actionMgr)

	mgr.ApiMgr.InitializeRestRoutes()
	mgr.ApiMgr.InitializeActionRestRoutes()
	mgr.ApiMgr.InitializeEventRestRoutes()
	mgr.ApiMgr.InstantiateRestRtr()

	//@TODO: this is bad as its global object... lets see what we can do with this
	futureObjKey = make(map[string][]int32, 50)
	mgr.bringUpTime = time.Now()
	// Initialize channel to receive connected client name.
	// When confd connects to a client, it creates global objects owned by that client and
	// stores default logging level in DB, if it does not exist.
	// Global objects and logging objects can only be updated by user.
	mgr.cltNameCh = make(chan string, 10)
	logger.Info("Initialization Done!")

	mgr.ReadSystemSwVersion()
	go mgr.AutoCreateConfigObjects()
	go mgr.clientMgr.ConnectToAllClients(mgr.cltNameCh)
	go mgr.clientMgr.ListenToClientStateChanges()
	go mgr.SigHandler()
	gConfigMgr = mgr

	return mgr
}

func (mgr *ConfigMgr) SigHandler() {
	sigChan := make(chan os.Signal, 1)
	signalList := []os.Signal{syscall.SIGHUP}
	signal.Notify(sigChan, signalList...)

	for {
		select {
		case signal := <-sigChan:
			switch signal {
			case syscall.SIGHUP:
				mgr.logger.Info("Exting!!!")
				os.Exit(0)
			default:
			}
		}
	}
}

/*
func GetSystemStatus() modelObjs.SystemStatusState {
	systemStatus := modelObjs.SystemStatusState{}
	systemStatus.Name, _ = os.Hostname()
	systemStatus.Ready = gConfigMgr.clientMgr.IsReady()
	if systemStatus.Ready == false {
		reason := "Not connected to"
		unconnectedClients := gConfigMgr.clientMgr.GetUnconnectedClients()
		for idx := 0; idx < len(unconnectedClients); idx++ {
			reason = reason + " " + unconnectedClients[idx]
		}
		systemStatus.Reason = reason
	} else {
		systemStatus.Reason = "None"
	}
	systemStatus.UpTime = time.Since(gConfigMgr.bringUpTime).String()
	systemStatus.NumCreateCalls =
		fmt.Sprintf("Total %d Success %d", gConfigMgr.ApiMgr.ApiCallStats.NumCreateCalls, gConfigMgr.ApiMgr.ApiCallStats.NumCreateCallsSuccess)
	systemStatus.NumDeleteCalls =
		fmt.Sprintf("Total %d Success %d", gConfigMgr.ApiMgr.ApiCallStats.NumDeleteCalls, gConfigMgr.ApiMgr.ApiCallStats.NumDeleteCallsSuccess)
	systemStatus.NumUpdateCalls =
		fmt.Sprintf("Total %d Success %d", gConfigMgr.ApiMgr.ApiCallStats.NumUpdateCalls, gConfigMgr.ApiMgr.ApiCallStats.NumUpdateCallsSuccess)
	systemStatus.NumGetCalls =
		fmt.Sprintf("Total %d Success %d", gConfigMgr.ApiMgr.ApiCallStats.NumGetCalls, gConfigMgr.ApiMgr.ApiCallStats.NumGetCallsSuccess)
	systemStatus.NumActionCalls =
		fmt.Sprintf("Total %d Success %d", gConfigMgr.ApiMgr.ApiCallStats.NumActionCalls, gConfigMgr.ApiMgr.ApiCallStats.NumActionCallsSuccess)

	// Read DaemonStates from db
	var daemonState modelObjs.DaemonState
	gConfigMgr.dbHdl.DbLock.Lock()
	daemonStates, _ := daemonState.GetAllObjFromDb(gConfigMgr.dbHdl)
	gConfigMgr.dbHdl.DbLock.Unlock()
	systemStatus.FlexDaemons = make([]modelObjs.DaemonState, len(daemonStates))
	for idx, daemonState := range daemonStates {
		systemStatus.FlexDaemons[idx] = daemonState.(modelObjs.DaemonState)
	}
	return systemStatus
}

func GetSystemSwVersion() modelObjs.SystemSwVersionState {
	systemSwVersion := modelObjs.SystemSwVersionState{}
	systemSwVersion.FlexswitchVersion = gConfigMgr.swVersion.SwVersion
	numRepos := len(gConfigMgr.swVersion.Repos)
	systemSwVersion.Repos = make([]modelObjs.RepoInfo, numRepos)
	for i := 0; i < numRepos; i++ {
		systemSwVersion.Repos[i].Name = gConfigMgr.swVersion.Repos[i].Name
		systemSwVersion.Repos[i].Sha1 = gConfigMgr.swVersion.Repos[i].Sha1
		systemSwVersion.Repos[i].Branch = gConfigMgr.swVersion.Repos[i].Branch
		systemSwVersion.Repos[i].Time = gConfigMgr.swVersion.Repos[i].Time
	}
	return systemSwVersion
}

func (mgr *ConfigMgr) DiscoverPorts() error {
	mgr.logger.Debug("Discovering ports")
	// Get ports present on this system and store in DB for user to update port parameters
	resource := "Port"
	if objHdl, ok := modelObjs.ConfigObjectMap[resource]; ok {
		var objs []modelObjs.ConfigObj
		var err error
		_, obj, _ := objects.GetConfigObj(nil, objHdl)
		currentIndex := int64(asicdCommonDefs.MIN_SYS_PORTS)
		objCount := int64(asicdCommonDefs.MAX_SYS_PORTS)
		resourceOwner := mgr.objectMgr.ObjHdlMap[resource].Owner
		defer resourceOwner.UnlockApiHandler()
		resourceOwner.LockApiHandler()
		err, _, _, _, objs = resourceOwner.GetBulkObject(obj, mgr.dbHdl.DBUtil, currentIndex, objCount)
		if err == nil {
			var LinkedObjects []string
			for key, value := range mgr.objectMgr.ObjHdlMap {
				if key != resource {
					continue
				}
				LinkedObjects = value.LinkedObjects
			}
			for i := 0; i < len(objs); i++ {
				portConfig := (*objs[i].(*modelObjs.Port))
				gConfigMgr.dbHdl.DbLock.Lock()
				_, err := portConfig.GetObjectFromDb(portConfig.GetKey(), mgr.dbHdl)
				gConfigMgr.dbHdl.DbLock.Unlock()
				// if we can not find the port in DB then go ahead and store
				if err != nil {
					gConfigMgr.dbHdl.DbLock.Lock()
					err = portConfig.StoreObjectInDb(mgr.dbHdl)
					gConfigMgr.dbHdl.DbLock.Unlock()
					if err != nil {
						mgr.logger.Err(fmt.Sprintln("Failed to store Port in DB ",
							i, portConfig, err))
					} else {
						mgr.storeUUID(portConfig.GetKey())
						for _, linkedObj := range LinkedObjects {
							keys := futureObjKey[linkedObj]
							keys = append(keys, portConfig.IfIndex)
							futureObjKey[linkedObj] = keys
						}
					}
				}
			}
		}
	}
	mgr.logger.Debug("Ports discovered")
	return nil
}
*/

func (mgr *ConfigMgr) storeUUID(key string) {
	_, err := mgr.dbHdl.StoreUUIDToObjKeyMap(key)
	if err != nil {
		mgr.logger.Err(fmt.Sprintln(
			"Failed to store uuid map for key ", key, err))
	}
}

/*
func (mgr *ConfigMgr) ConfigureGlobalConfig(paramsDir, key string, client clients.ClientIf) {
	var obj modelObjs.ConfigObj
	var err error
	mgr.logger.Info(fmt.Sprintln("Object: ", key, "is global object"))
	if objHdl, ok := modelObjs.ConfigObjectMap[key]; ok {
		var body []byte // @dummy body for default objects
		obj, _ = objHdl.UnmarshalObject(body)
		_, err = objHdl.GetObjectFromDb(obj.GetKey(), mgr.dbHdl)
		// @TODO: AVOY/HARI we need to fix default value for key... today we do not support default value for
		//keys
		if err != nil {
			var success bool
			// If no object found then we need to call daemons with default parameters...
			// SystemParam is unique case where we will use SystemProfile.json to parse the
			// information
			if key == "SystemParam" {
				sysBody := mgr.ConstructSystemParam()
				sysObj, _ := objHdl.UnmarshalObject(sysBody)
				err, success = client.CreateObject(sysObj, mgr.dbHdl.DBUtil)
				if err == nil && success == true {
					mgr.storeUUID(sysObj.GetKey())
				}
			} else {
				keys, exists := futureObjKey[key]
				if exists {
					// Special case for linked objects...
					for _, ifIndex := range keys {
						switch key {
						case "LLDPIntf": // @TODO: this is bad... as its hardcoded :(
							lldpObj := &modelObjs.LLDPIntf{}
							lldpObj.IfIndex = ifIndex
							bytes, err := json.Marshal(lldpObj)
							lldpIntfObj, _ := objHdl.UnmarshalObject(bytes)
							err, success = client.CreateObject(lldpIntfObj, mgr.dbHdl.DBUtil)
							if err == nil && success == true {
								mgr.storeUUID(lldpIntfObj.GetKey())
							}
						}
					}
				} else {
					err, success = client.CreateObject(obj, mgr.dbHdl.DBUtil)
					if err == nil && success == true {
						mgr.storeUUID(obj.GetKey())
					}
				}
			}
		} else {
			_, err = mgr.dbHdl.GetUUIDFromObjKey(obj.GetKey())
			if err != nil {
				mgr.storeUUID(obj.GetKey())
			}
		}
	}
}
*/

func (mgr *ConfigMgr) ConfigureGlobalConfig(clientName string) {
	var obj modelObjs.ConfigObj
	var err error
	if ent, ok := mgr.objectMgr.AutoCreateObjMap[clientName]; ok {
		for _, resource := range ent.ObjList {
			if objHdl, ok := modelObjs.ConfigObjectMap[resource]; ok {
				var body []byte // @dummy body for default objects
				obj, _ = objHdl.UnmarshalObject(body)
				_, err = objHdl.GetObjectFromDb(obj.GetKey(), mgr.dbHdl)
				if err != nil {
					client, exist := mgr.clientMgr.Clients[clientName]
					if exist {
						err, success := client.CreateObject(obj, mgr.dbHdl.DBUtil)
						if err == nil && success == true {
							mgr.storeUUID(obj.GetKey())
						} else {
							mgr.logger.Err(fmt.Sprintln("Failed to create "+resource+" ", obj, err))
						}
					}
				}
			}
		}
	}
}

func (mgr *ConfigMgr) AutoCreateConfigObjects() {
	for {
		select {
		case clientName := <-mgr.cltNameCh:
			switch clientName {
			case "Client_Init_Done":
				close(mgr.cltNameCh)
				return
				/*
					case "asicd":
						mgr.DiscoverPorts()
				*/
			default:
				mgr.logger.Info("Do Global Init and Discover objects for Client: " + clientName)
				mgr.ConstructSystemParam(clientName)
				mgr.ConfigureGlobalConfig(clientName)
				mgr.AutoDiscoverObjects(clientName)
				mgr.ConfigureComponentLoggingLevel(clientName)
			}
		}
	}
}

func (mgr *ConfigMgr) AutoDiscoverObjects(clientName string) {
	fmt.Println("AutoDiscover for: ", clientName)
	if ent, ok := mgr.objectMgr.AutoDiscoverObjMap[clientName]; ok {
		for _, resource := range ent.ObjList {
			fmt.Println("AutoDiscover: ", resource)
			if objHdl, ok := modelObjs.ConfigObjectMap[resource]; ok {
				var objs []modelObjs.ConfigObj
				var err error
				_, obj, _ := objects.GetConfigObj(nil, objHdl)
				currentIndex := int64(0)
				objCount := int64(MAX_COUNT_AUTO_DISCOVER_OBJ)
				err, _, _, _, objs = mgr.objectMgr.ObjHdlMap[resource].Owner.GetBulkObject(obj, mgr.dbHdl.DBUtil,
					currentIndex, objCount)
				fmt.Println("AutoDiscover response: ", err, objs)
				if err == nil {
					for _, obj := range objs {
						_, err := obj.GetObjectFromDb(obj.GetKey(), mgr.dbHdl)
						if err != nil {
							err = obj.StoreObjectInDb(mgr.dbHdl)
							if err != nil {
								mgr.logger.Err(fmt.Sprintln("Failed to store"+resource+" config in DB ", obj, err))
							} else {
								mgr.storeUUID(obj.GetKey())
							}
						}
					}
				}
			}
		}
	}
}
