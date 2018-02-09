package main

import (
	"encoding/json"
	es "github.com/launchdarkly/eventsource"
	ld "gopkg.in/launchdarkly/go-client.v2"
	"time"
)

type SSERelayFeatureStore struct {
	store          ld.FeatureStore
	allPublisher   *es.Server
	flagsPublisher *es.Server
	apiKey         string
}

type allRepository struct {
	relayStore *SSERelayFeatureStore
}
type flagsRepository struct {
	relayStore *SSERelayFeatureStore
}

func NewSSERelayFeatureStore(apiKey string, allPublisher *es.Server, flagsPublisher *es.Server, baseFeatureStore ld.FeatureStore, heartbeatInterval int) *SSERelayFeatureStore {
	relayStore := &SSERelayFeatureStore{
		store:          baseFeatureStore,
		apiKey:         apiKey,
		allPublisher:   allPublisher,
		flagsPublisher: flagsPublisher,
	}

	allPublisher.Register(apiKey, allRepository{relayStore})
	flagsPublisher.Register(apiKey, flagsRepository{relayStore})

	if heartbeatInterval > 0 {
		go func() {
			t := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
			for {
				relayStore.heartbeat()
				<-t.C
			}
		}()
	}

	return relayStore
}

func (relay *SSERelayFeatureStore) keys() []string {
	return []string{relay.apiKey}
}

func (relay *SSERelayFeatureStore) heartbeat() {
	relay.allPublisher.Publish(relay.keys(), heartbeatEvent("hb"))
	relay.flagsPublisher.Publish(relay.keys(), heartbeatEvent("hb"))
}

func (relay *SSERelayFeatureStore) Get(key string) (*ld.FeatureFlag, error) {
	return relay.store.Get(key)
}

func (relay *SSERelayFeatureStore) All() (map[string]*ld.FeatureFlag, error) {
	return relay.store.All()
}

func (relay *SSERelayFeatureStore) Init(flags map[string]*ld.FeatureFlag) error {
	err := relay.store.Init(flags)

	if err != nil {
		return err
	}

	relay.allPublisher.Publish(relay.keys(), makePutEvent(flags))
	relay.flagsPublisher.Publish(relay.keys(), makeFlagsPutEvent(flags))

	return nil
}

func (relay *SSERelayFeatureStore) Delete(key string, version int) error {
	err := relay.store.Delete(key, version)
	if err != nil {
		return err
	}

	relay.allPublisher.Publish(relay.keys(), makeDeleteEvent(key, version))
	relay.flagsPublisher.Publish(relay.keys(), makeFlagsDeleteEvent(key, version))

	return nil
}

func (relay *SSERelayFeatureStore) Upsert(key string, f ld.FeatureFlag) error {
	err := relay.store.Upsert(key, f)

	if err != nil {
		return err
	}

	flag, err := relay.store.Get(key)

	if err != nil {
		return err
	}

	if flag != nil {
		relay.allPublisher.Publish(relay.keys(), makeUpsertEvent(*flag))
		relay.flagsPublisher.Publish(relay.keys(), makeFlagsUpsertEvent(*flag))
	}

	return nil
}

func (relay *SSERelayFeatureStore) Initialized() bool {
	return relay.store.Initialized()
}

// Allows the feature store to act as an SSE repository (to send bootstrap events)
func (r flagsRepository) Replay(channel, id string) (out chan es.Event) {
	out = make(chan es.Event)
	go func() {
		defer close(out)
		if r.relayStore.Initialized() {
			flags, err := r.relayStore.All()

			if err != nil {
				Error.Printf("Error getting all flags: %s\n", err.Error())
			} else {
				out <- makeFlagsPutEvent(flags)
			}
		}
	}()
	return
}

func (r allRepository) Replay(channel, id string) (out chan es.Event) {
	out = make(chan es.Event)
	go func() {
		defer close(out)
		if r.relayStore.Initialized() {
			flags, err := r.relayStore.All()

			if err != nil {
				Error.Printf("Error getting all flags: %s\n", err.Error())
			} else {
				out <- makePutEvent(flags)
			}
		}
	}()
	return
}

type flagsPutEvent map[string]*ld.FeatureFlag
type allPutEvent map[string]map[string]interface{}

type deleteEvent struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
}

type upsertEvent struct {
	Path string         `json:"path"`
	D    ld.FeatureFlag `json:"data"`
}

type heartbeatEvent string

func (h heartbeatEvent) Id() string {
	return ""
}

func (h heartbeatEvent) Event() string {
	return ""
}

func (h heartbeatEvent) Data() string {
	return ""
}

func (h heartbeatEvent) Comment() string {
	return string(h)
}

func (t flagsPutEvent) Id() string {
	return ""
}

func (t flagsPutEvent) Event() string {
	return "put"
}

func (t flagsPutEvent) Data() string {
	data, _ := json.Marshal(t)

	return string(data)
}

func (t flagsPutEvent) Comment() string {
	return ""
}

func (t allPutEvent) Id() string {
	return ""
}

func (t allPutEvent) Event() string {
	return "put"
}

func (t allPutEvent) Data() string {
	data, _ := json.Marshal(t)

	return string(data)
}

func (t allPutEvent) Comment() string {
	return ""
}

func (t upsertEvent) Id() string {
	return ""
}

func (t upsertEvent) Event() string {
	return "patch"
}

func (t upsertEvent) Data() string {
	data, _ := json.Marshal(t)

	return string(data)
}

func (t upsertEvent) Comment() string {
	return ""
}

func (t deleteEvent) Id() string {
	return ""
}

func (t deleteEvent) Event() string {
	return "delete"
}

func (t deleteEvent) Data() string {
	data, _ := json.Marshal(t)

	return string(data)
}

func (t deleteEvent) Comment() string {
	return ""
}

func makeUpsertEvent(f ld.FeatureFlag) es.Event {
	return upsertEvent{
		Path: "/" + "flags" + "/" + f.Key,
		D:    f,
	}
}

func makeFlagsUpsertEvent(f ld.FeatureFlag) es.Event {
	return upsertEvent{
		Path: "/" + f.Key,
		D:    f,
	}
}

func makeDeleteEvent(key string, version int) es.Event {
	return deleteEvent{
		Path:    "/" + "flags" + "/" + key,
		Version: version,
	}
}

func makeFlagsDeleteEvent(key string, version int) es.Event {
	return deleteEvent{
		Path:    "/" + key,
		Version: version,
	}
}

func makePutEvent(flags map[string]*ld.FeatureFlag) es.Event {
	allData := make(map[string]map[string]interface{})
	for key, flag := range flags {
		allData["flags"][key] = flag
	}
	allData["segments"] = make(map[string]interface{})
	return allPutEvent(allData)
}

func makeFlagsPutEvent(flags map[string]*ld.FeatureFlag) es.Event {
	return flagsPutEvent(flags)
}
