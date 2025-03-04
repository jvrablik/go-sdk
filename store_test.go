package statsig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreSync(t *testing.T) {
	type requestCounter struct {
		configsCount int
		idlistsCount int
		list1Count   int
		list2Count   int
		list3Count   int
	}
	counter := &requestCounter{}

	testServer := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(http.StatusOK)
		if strings.Contains(req.URL.Path, "download_config_specs") {
			var in *downloadConfigsInput
			var r *downloadConfigSpecResponse
			json.NewDecoder(req.Body).Decode(&in)
			if counter.configsCount == 0 {
				r = &downloadConfigSpecResponse{
					HasUpdates:     true,
					Time:           time.Now().Unix(),
					FeatureGates:   []configSpec{{Name: "gate_1"}},
					DynamicConfigs: []configSpec{{Name: "exp_1"}},
				}
			} else {
				r = &downloadConfigSpecResponse{
					HasUpdates:     true,
					Time:           time.Now().Unix(),
					FeatureGates:   []configSpec{{Name: "gate_1"}, {Name: "gate_2"}},
					DynamicConfigs: []configSpec{{Name: "exp_1"}, {Name: "exp_2"}},
				}
			}
			v, _ := json.Marshal(r)
			res.Write(v)
			counter.configsCount++
		} else if strings.Contains(req.URL.Path, "get_id_lists") {
			var r map[string]idList
			baseURL := "http://" + req.Host
			switch counter.idlistsCount {
			case 0:
				r = map[string]idList{
					"list_1": {Name: "list_1", Size: 3, URL: baseURL + "/list_1", CreationTime: 1, FileID: "file_id_1"},
					"list_2": {Name: "list_1", Size: 3, URL: baseURL + "/list_2", CreationTime: 1, FileID: "file_id_2"},
				}
			case 1:
				r = map[string]idList{
					// size increased
					"list_1": {Name: "list_1", Size: 9, URL: baseURL + "/list_1", CreationTime: 1, FileID: "file_id_1"},
					// list_2 deleted
				}
			case 2:
				r = map[string]idList{
					// new file
					"list_1": {Name: "list_1", Size: 3, URL: baseURL + "/list_1", CreationTime: 3, FileID: "file_id_1_a"},
				}
			case 3:
				r = map[string]idList{
					// returned old file due to some issue
					"list_1": {Name: "list_1", Size: 9, URL: baseURL + "/list_1", CreationTime: 1, FileID: "file_id_1"},
				}
			default:
				r = map[string]idList{
					// back to the new file, and size increased
					"list_1": {Name: "list_1", Size: 18, URL: baseURL + "/list_1", CreationTime: 3, FileID: "file_id_1_a"},
					// list_3 added
					"list_3": {Name: "list_3", Size: 3, URL: baseURL + "/list_3", CreationTime: 5, FileID: "file_id_3"},
				}
			}
			v, _ := json.Marshal(r)
			res.Write(v)
			counter.idlistsCount++
		} else if strings.Contains(req.URL.Path, "list_1") {
			var r string
			switch counter.list1Count {
			case 0:
				r = "+1\n"
			case 1:
				r = "-1\n+2\n"
			case 2:
				r = "+3\n"
			case 3:
				r = "3"
			default:
				r = "+3\n+4\n+5\n+4\n-4\n+6\n"
			}
			res.Write([]byte(r))
			counter.list1Count++
		} else if strings.Contains(req.URL.Path, "list_2") {
			res.Write([]byte("+a\n"))
			counter.list2Count++
		} else if strings.Contains(req.URL.Path, "list_3") {
			res.Write([]byte("+0\n"))
			counter.list3Count++
		}
	}))

	defer testServer.Close()
	opt := &Options{
		API: testServer.URL,
	}
	n := newTransport("secret-123", opt)
	s := newStoreInternal(n, time.Second, time.Second)

	if len(s.featureGates) != 1 {
		t.Errorf("Wrong number of feature gates after initialize")
	}
	if len(s.dynamicConfigs) != 1 {
		t.Errorf("Wrong number of configs after initialize")
	}

	if len(s.idLists) != 2 {
		t.Errorf("Wrong number of id lists after initialize")
	}
	if !compareIDLists(s.getIDList("list_1"),
		&idList{Name: "list_1", Size: 3, URL: testServer.URL + "/list_1", CreationTime: 1, FileID: "file_id_1", ids: *idListMapToSyncMap(map[string]bool{"1": true})}) {
		t.Errorf("list_1 is incorrect after initialize")
	}
	if !compareIDLists(s.getIDList("list_2"),
		&idList{Name: "list_2", Size: 3, URL: testServer.URL + "/list_2", CreationTime: 1, FileID: "file_id_2", ids: *idListMapToSyncMap(map[string]bool{"a": true})}) {
		t.Errorf("list_2 is incorrect after initialize")
	}
	if s.getIDList("list_3") != nil {
		t.Errorf("list_3 should be nil after initialize")
	}

	if counter.configsCount != 1 {
		t.Errorf("download_config_specs should have been called 1 time")
	}
	if counter.idlistsCount != 1 {
		t.Errorf("get_id_lists should have been called 1 time")
	}
	if counter.list1Count != 1 || counter.list2Count != 1 || counter.list3Count != 0 {
		t.Errorf("individual id list download count is incorrect after initialize")
	}

	time.Sleep(time.Millisecond * 1100)
	if !compareIDLists(s.getIDList("list_1"),
		&idList{Name: "list_1", Size: 9, URL: testServer.URL + "/list_1", CreationTime: 1, FileID: "file_id_1", ids: *idListMapToSyncMap(map[string]bool{"2": true})}) {
		t.Errorf("list_1 is incorrect after 1 second")
	}
	if s.getIDList("list_2") != nil {
		t.Errorf("list_2 should be nil after 1 second")
	}
	if s.getIDList("list_3") != nil {
		t.Errorf("list_3 should be nil after 1 second")
	}

	if counter.configsCount != 2 {
		t.Errorf("download_config_specs should have been called 2 times")
	}
	if counter.idlistsCount != 2 {
		t.Errorf("get_id_lists should have been called 2 times")
	}
	if counter.list1Count != 2 || counter.list2Count != 1 || counter.list3Count != 0 {
		t.Errorf("individual id list download count is incorrect after 1 second")
	}

	time.Sleep(time.Millisecond * 1100)
	if !compareIDLists(s.getIDList("list_1"),
		&idList{Name: "list_1", Size: 3, URL: testServer.URL + "/list_1", CreationTime: 3, FileID: "file_id_1_a", ids: *idListMapToSyncMap(map[string]bool{"3": true})}) {
		t.Errorf("list_1 is incorrect after 2 seconds")
	}
	if s.getIDList("list_2") != nil {
		t.Errorf("list_2 should be nil after 2 seconds")
	}
	if s.getIDList("list_3") != nil {
		t.Errorf("list_3 should be nil after 2 seconds")
	}

	if counter.configsCount != 3 {
		t.Errorf("download_config_specs should have been called 3 times")
	}
	if counter.idlistsCount != 3 {
		t.Errorf("get_id_lists should have been called 3 times")
	}
	if counter.list1Count != 3 || counter.list2Count != 1 || counter.list3Count != 0 {
		t.Errorf("individual id list download count is incorrect after 2 seconds")
	}

	time.Sleep(time.Millisecond * 1100)
	if !compareIDLists(s.getIDList("list_1"),
		&idList{Name: "list_1", Size: 3, URL: testServer.URL + "/list_1", CreationTime: 3, FileID: "file_id_1_a", ids: *idListMapToSyncMap(map[string]bool{"3": true})}) {
		t.Errorf("list_1 should NOT have changed after 3 seconds because response was pointing to the older url")
	}
	if s.getIDList("list_2") != nil {
		t.Errorf("list_2 should be nil after 3 seconds")
	}
	if s.getIDList("list_3") != nil {
		t.Errorf("list_3 should be nil after 3 seconds")
	}

	if counter.configsCount != 4 {
		t.Errorf("download_config_specs should have been called 4 times")
	}
	if counter.idlistsCount != 4 {
		t.Errorf("get_id_lists should have been called 4 times")
	}
	if counter.list1Count != 3 || counter.list2Count != 1 || counter.list3Count != 0 {
		t.Errorf("individual id list download count is incorrect after 3 seconds")
	}

	time.Sleep(time.Millisecond * 1100)
	if s.getIDList("list_1") != nil {
		t.Errorf("list_1 should be nil after 4 seconds because response was corrupted")
	}
	if s.getIDList("list_2") != nil {
		t.Errorf("list_2 should be nil after 4 seconds")
	}
	if !compareIDLists(s.getIDList("list_3"),
		&idList{Name: "list_3", Size: 3, URL: testServer.URL + "/list_3", CreationTime: 5, FileID: "file_id_3", ids: *idListMapToSyncMap(map[string]bool{"0": true})}) {
		t.Errorf("list_3 should not be nil anymore after 4 seconds")
	}

	if counter.configsCount != 5 {
		t.Errorf("download_config_specs should have been called 5 times")
	}
	if counter.idlistsCount != 5 {
		t.Errorf("get_id_lists should have been called 5 times")
	}
	if counter.list1Count != 4 || counter.list2Count != 1 || counter.list3Count != 1 {
		t.Errorf("individual id list download count is incorrect after 4 seconds")
	}

	time.Sleep(time.Millisecond * 1100)
	if !compareIDLists(s.getIDList("list_1"),
		&idList{Name: "list_1", Size: 18, URL: testServer.URL + "/list_1", CreationTime: 3, FileID: "file_id_1_a", ids: *idListMapToSyncMap(map[string]bool{"3": true, "5": true, "6": true})}) {
		t.Errorf("list_1 is incorrect after 5 seconds")
	}
	if s.getIDList("list_2") != nil {
		t.Errorf("list_2 should be nil after 5 seconds")
	}
	if !compareIDLists(s.getIDList("list_3"),
		&idList{Name: "list_3", Size: 3, URL: testServer.URL + "/list_3", CreationTime: 5, FileID: "file_id_3", ids: *idListMapToSyncMap(map[string]bool{"0": true})}) {
		t.Errorf("list_3 is incorrect after 5 seconds")
	}

	if counter.configsCount != 6 {
		t.Errorf("download_config_specs should have been called 6 times")
	}
	if counter.idlistsCount != 6 {
		t.Errorf("get_id_lists should have been called 6 times")
	}
	if counter.list1Count != 5 || counter.list2Count != 1 || counter.list3Count != 1 {
		t.Errorf("individual id list download count is incorrect after 5 seconds")
	}
}

func compareIDLists(l1 *idList, l2 *idList) bool {
	if l1.Name != l2.Name || l1.Size != l2.Size || l1.URL != l2.URL || l1.CreationTime != l2.CreationTime || l1.FileID != l2.FileID {
		return false
	}
	ids1 := unsyncIDList(&l1.ids)
	ids2 := unsyncIDList(&l2.ids)
	return reflect.DeepEqual(ids1, ids2)
}

func unsyncIDList(m *sync.Map) map[string]bool {
	mm := make(map[string]bool)
	m.Range(func(k, v interface{}) bool {
		mm[k.(string)] = true
		return true
	})
	return mm
}

func idListMapToSyncMap(m map[string]bool) *sync.Map {
	mm := sync.Map{}
	for k, _ := range m {
		mm.Store(k, true)
	}
	return &mm
}
