package agollo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Client for apollo
type Client struct {
	conf *Conf

	updateChan chan *ChangeEvent

	caches         *namespaceCache
	releaseKeyRepo *cache

	longPoller poller
	requester  requester

	ctx    context.Context
	cancel context.CancelFunc
}

// result of query config
type result struct {
	// AppID          string            `json:"appId"`
	// Cluster        string            `json:"cluster"`
	NamespaceName  string            `json:"namespaceName"`
	Configurations map[string]string `json:"configurations"`
	ReleaseKey     string            `json:"releaseKey"`
}

// NewClient create client from conf
func NewClient(conf *Conf) *Client {
	client := &Client{
		conf:           conf,
		caches:         newNamespaceCahce(),
		releaseKeyRepo: newCache(),

		requester: newHTTPRequester(&http.Client{Timeout: queryTimeout}),
	}

	client.longPoller = newLongPoller(conf, longPoolInterval, client.handleNamespaceUpdate)
	client.ctx, client.cancel = context.WithCancel(context.Background())
	return client
}

// Start sync config
func (c *Client) Start() error {

	// preload all config to local first
	if err := c.preload(); err != nil {
		return err
	}

	// start fetch update
	go c.longPoller.start()

	return nil
}

// handleNamespaceUpdate sync config for namespace, delivery changes to subscriber
func (c *Client) handleNamespaceUpdate(namespace string) error {
	change, err := c.sync(namespace)
	if err != nil || change == nil {
		return err
	}

	c.deliveryChangeEvent(change)
	return nil
}

// Stop sync config
func (c *Client) Stop() error {
	c.longPoller.stop()
	c.cancel()
	// close(c.updateChan)
	c.updateChan = nil
	return nil
}

// fetchAllCinfig fetch from remote, if failed load from local file
func (c *Client) preload() error {
	if err := c.longPoller.preload(); err != nil {
		return c.loadLocal(c.conf.Env + defaultDumpFile)
	}
	return nil
}

// loadLocal load caches from local file
func (c *Client) loadLocal(name string) error {
	return c.caches.load(name)
}

// dump caches to file
func (c *Client) dump(name string) error {
	return c.caches.dump(name)
}

// WatchUpdate get all updates
func (c *Client) WatchUpdate() <-chan *ChangeEvent {
	if c.updateChan == nil {
		c.updateChan = make(chan *ChangeEvent)
	}
	return c.updateChan
}

func (c *Client) mustGetCache(namespace string) *cache {
	return c.caches.mustGetCache(namespace)
}

// GetStringValueWithNameSapce get value from given namespace
func (c *Client) GetStringValueWithNameSpace(namespace, key, defaultValue string) string {
	cache := c.mustGetCache(namespace)
	if ret, ok := cache.get(key); ok && ret != "" {
		return ret
	}
	return defaultValue
}

// GetStringValue from default namespace
func (c *Client) GetStringValue(key, defaultValue string) string {
	return c.GetStringValueWithNameSpace(defaultNamespace, key, defaultValue)
}

// GetNameSpaceContent get contents of namespace
func (c *Client) GetNameSpaceContent(namespace, defaultValue string) string {

	cache := c.mustGetCache(namespace)
	kv := cache.dump()
	res, _ := json.Marshal(kv)
	return string(res)
}

func (c *Client) GetNameSpaceContentMap(namespace string) map[string]string {

	cache := c.mustGetCache(namespace)
	return cache.dump()
}

func (c *Client) GetNameSpaceContentMapMerge(namespace string) map[string]map[string]string {
	mapData := c.GetNameSpaceContentMap(namespace)
	mapMergeData := make(map[string]map[string]string)
	if mapData != nil {
		for k, v := range mapData {
			keys := strings.Split(k, ".")
			if _, ok := mapMergeData[keys[0]]; !ok {
				mapMergeData[keys[0]] = make(map[string]string)
			}
			if len(keys) > 1 {
				mapMergeData[keys[0]][keys[1]] = v
			}
		}
	}

	return mapMergeData
}

func (c *Client) GetNameSpaceContentMapMergeTree(namespace string) map[string][]map[string]string {
	mapMergeData := c.GetNameSpaceContentMapMerge(namespace)
	result := make(map[string][]map[string]string)
	if mapMergeData != nil {
		for key, v := range mapMergeData {
			subKeys := strings.Split(key, "_")
			if _, ok := result[subKeys[0]]; !ok {
				result[subKeys[0]] = make([]map[string]string, 0)
			}
			result[subKeys[0]] = append(result[subKeys[0]], v)
		}
	}

	return result
}

// sync namespace config
func (c *Client) sync(namesapce string) (*ChangeEvent, error) {
	releaseKey := c.getReleaseKey(namesapce)
	url := configURL(c.conf, namesapce, releaseKey)
	bts, err := c.requester.request(url)
	if err != nil || len(bts) == 0 {
		return nil, err
	}

	var result result
	if err := json.Unmarshal(bts, &result); err != nil {
		return nil, err
	}

	return c.handleResult(&result), nil
}

// deliveryChangeEvent push change to subscriber
func (c *Client) deliveryChangeEvent(change *ChangeEvent) {
	if c.updateChan == nil {
		return
	}
	select {
	case <-c.ctx.Done():
	case c.updateChan <- change:
	}
}

// handleResult generate changes from query result, and update local cache
func (c *Client) handleResult(result *result) *ChangeEvent {
	var ret = ChangeEvent{
		Namespace: result.NamespaceName,
		Changes:   map[string]*Change{},
	}
	cache := c.mustGetCache(result.NamespaceName)
	kv := cache.dump()

	for k, v := range kv {
		if _, ok := result.Configurations[k]; !ok {
			cache.delete(k)
			ret.Changes[k] = makeDeleteChange(k, v)
		}
	}

	for k, v := range result.Configurations {
		cache.set(k, v)
		old, ok := kv[k]
		if !ok {
			ret.Changes[k] = makeAddChange(k, v)
			continue
		}
		if old != v {
			ret.Changes[k] = makeModifyChange(k, old, v)
		}
	}

	c.setReleaseKey(result.NamespaceName, result.ReleaseKey)

	// dump caches to file
	c.dump(defaultDumpFile)

	if len(ret.Changes) == 0 {
		return nil
	}

	return &ret
}

func (c *Client) getReleaseKey(namespace string) string {
	releaseKey, _ := c.releaseKeyRepo.get(namespace)
	return releaseKey
}

func (c *Client) setReleaseKey(namespace, releaseKey string) {
	c.releaseKeyRepo.set(namespace, releaseKey)
}
