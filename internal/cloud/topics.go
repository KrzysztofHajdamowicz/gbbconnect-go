package cloud

import "fmt"

const (
	toDeviceSuffix   = "ModbusInMqtt/toDevice"
	fromDeviceSuffix = "ModbusInMqtt/fromDevice"
	keepaliveSuffix  = "keepalive"
)

// ToDeviceTopic returns the cloud-to-device Modbus request topic.
func ToDeviceTopic(plantID string) string {
	return fmt.Sprintf("%s/%s", plantID, toDeviceSuffix)
}

// FromDeviceTopic returns the device-to-cloud Modbus response topic.
func FromDeviceTopic(plantID string) string {
	return fmt.Sprintf("%s/%s", plantID, fromDeviceSuffix)
}

// KeepaliveTopic returns the plant heartbeat topic.
func KeepaliveTopic(plantID string) string {
	return fmt.Sprintf("%s/%s", plantID, keepaliveSuffix)
}
