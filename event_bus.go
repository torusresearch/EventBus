package EventBus

import (
	"fmt"
	"reflect"
	"sync"
)

//BusSubscriber defines subscription-related bus behavior
type BusSubscriber interface {
	Subscribe(topic string, fn func(interface{})) error
	SubscribeAsync(topic string, fn func(interface{}), transactional bool) error
	SubscribeOnce(topic string, fn func(interface{})) error
	SubscribeOnceAsync(topic string, fn func(interface{})) error
	Unsubscribe(topic string, handler func(interface{})) error
}

//BusPublisher defines publishing-related bus behavior
type BusPublisher interface {
	Publish(topic string, data interface{})
}

//BusController defines bus control behavior (checking handler's presence, synchronization)
type BusController interface {
	HasCallback(topic string) bool
	WaitAsync()
}

//Bus englobes global (subscribe, publish, control) bus behavior
type Bus interface {
	BusController
	BusSubscriber
	BusPublisher
}

// EventBus - box for handlers and callbacks.
type EventBus struct {
	handlers map[string][]*eventHandler
	lock     sync.Mutex // a lock for the map
	wg       sync.WaitGroup
}

type eventHandler struct {
	callBack      func(interface{})
	flagOnce      bool
	async         bool
	transactional bool
	sync.Mutex    // lock for an event handler - useful for running async callbacks serially
}

// New returns new EventBus with empty handlers.
func New() Bus {
	b := &EventBus{
		make(map[string][]*eventHandler),
		sync.Mutex{},
		sync.WaitGroup{},
	}
	return Bus(b)
}

// doSubscribe handles the subscription logic and is utilized by the public Subscribe functions
func (bus *EventBus) doSubscribe(topic string, fn func(interface{}), handler *eventHandler) error {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	bus.handlers[topic] = append(bus.handlers[topic], handler)
	return nil
}

// Subscribe subscribes to a topic.
// Returns error if `fn` is not a function.
func (bus *EventBus) Subscribe(topic string, fn func(interface{})) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		fn, false, false, false, sync.Mutex{},
	})
}

// SubscribeAsync subscribes to a topic with an asynchronous callback
// Transactional determines whether subsequent callbacks for a topic are
// run serially (true) or concurrently (false)
// Returns error if `fn` is not a function.
func (bus *EventBus) SubscribeAsync(topic string, fn func(interface{}), transactional bool) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		fn, false, true, transactional, sync.Mutex{},
	})
}

// SubscribeOnce subscribes to a topic once. Handler will be removed after executing.
// Returns error if `fn` is not a function.
func (bus *EventBus) SubscribeOnce(topic string, fn func(interface{})) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		fn, true, false, false, sync.Mutex{},
	})
}

// SubscribeOnceAsync subscribes to a topic once with an asynchronous callback
// Handler will be removed after executing.
// Returns error if `fn` is not a function.
func (bus *EventBus) SubscribeOnceAsync(topic string, fn func(interface{})) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		fn, true, true, false, sync.Mutex{},
	})
}

// HasCallback returns true if exists any callback subscribed to the topic.
func (bus *EventBus) HasCallback(topic string) bool {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	_, ok := bus.handlers[topic]
	if ok {
		return len(bus.handlers[topic]) > 0
	}
	return false
}

// Unsubscribe removes callback defined for a topic.
// Returns error if there are no callbacks subscribed to the topic.
func (bus *EventBus) Unsubscribe(topic string, handler func(interface{})) error {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	if _, ok := bus.handlers[topic]; ok && len(bus.handlers[topic]) > 0 {
		bus.removeHandler(topic, bus.findHandlerIdx(topic, handler))
		return nil
	}
	return fmt.Errorf("topic %s doesn't exist", topic)
}

// Publish executes callback defined for a topic. Any additional argument will be transferred to the callback.
func (bus *EventBus) Publish(topic string, data interface{}) {
	bus.lock.Lock() // will unlock if handler is not found or always after setUpPublish
	defer bus.lock.Unlock()
	if handlers, ok := bus.handlers[topic]; ok && 0 < len(handlers) {
		// Handlers slice may be changed by removeHandler and Unsubscribe during iteration,
		// so make a copy and iterate the copied slice.
		copyHandlers := make([]*eventHandler, 0, len(handlers))
		copyHandlers = append(copyHandlers, handlers...)
		for i, handler := range copyHandlers {
			if handler.flagOnce {
				bus.removeHandler(topic, i)
			}
			if !handler.async {
				bus.doPublish(handler, topic, data)
			} else {
				bus.wg.Add(1)
				if handler.transactional {
					handler.Lock()
				}
				go bus.doPublishAsync(handler, topic, data)
			}
		}
	}
}

func (bus *EventBus) doPublish(handler *eventHandler, topic string, data interface{}) {
	handler.callBack(data)
}

func (bus *EventBus) doPublishAsync(handler *eventHandler, topic string, data interface{}) {
	defer bus.wg.Done()
	if handler.transactional {
		defer handler.Unlock()
	}
	bus.doPublish(handler, topic, data)
}

func (bus *EventBus) removeHandler(topic string, idx int) {
	if _, ok := bus.handlers[topic]; !ok {
		return
	}
	l := len(bus.handlers[topic])

	if !(0 <= idx && idx < l) {
		return
	}

	copy(bus.handlers[topic][idx:], bus.handlers[topic][idx+1:])
	bus.handlers[topic][l-1] = nil // or the zero value of T
	bus.handlers[topic] = bus.handlers[topic][:l-1]
}

func (bus *EventBus) findHandlerIdx(topic string, callback func(interface{})) int {
	if _, ok := bus.handlers[topic]; ok {
		for idx, handler := range bus.handlers[topic] {
			if reflect.ValueOf(handler.callBack) == reflect.ValueOf(callback) {
				return idx
			}
		}
	}
	return -1
}

// WaitAsync waits for all async callbacks to complete
func (bus *EventBus) WaitAsync() {
	bus.wg.Wait()
}
