package cloud

import mqtt "github.com/eclipse/paho.mqtt.golang"

type mqttToken interface {
	Done() <-chan struct{}
	Error() error
}

type mqttBackend interface {
	IsConnected() bool
	Connect() mqttToken
	Subscribe(topic string, qos byte, handler MessageHandler) mqttToken
	Publish(topic string, qos byte, retained bool, payload []byte) mqttToken
	Disconnect(quiesce uint)
}

type backendFactory func(options *mqtt.ClientOptions) mqttBackend

type pahoBackend struct {
	client mqtt.Client
}

func newPahoBackend(options *mqtt.ClientOptions) mqttBackend {
	return &pahoBackend{client: mqtt.NewClient(options)}
}

func (backend *pahoBackend) IsConnected() bool {
	return backend.client.IsConnected()
}

func (backend *pahoBackend) Connect() mqttToken {
	return backend.client.Connect()
}

func (backend *pahoBackend) Subscribe(
	topic string,
	qos byte,
	handler MessageHandler,
) mqttToken {
	return backend.client.Subscribe(topic, qos, func(_ mqtt.Client, message mqtt.Message) {
		handler(message.Topic(), message.Payload())
	})
}

func (backend *pahoBackend) Publish(
	topic string,
	qos byte,
	retained bool,
	payload []byte,
) mqttToken {
	return backend.client.Publish(topic, qos, retained, payload)
}

func (backend *pahoBackend) Disconnect(quiesce uint) {
	backend.client.Disconnect(quiesce)
}
