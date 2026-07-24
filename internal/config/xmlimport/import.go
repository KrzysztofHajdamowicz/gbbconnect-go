// Package xmlimport imports legacy GbbConnect2 Parameters.xml files.
package xmlimport

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
)

// Import decodes a legacy Parameters.xml document.
func Import(reader io.Reader) (config.Config, []string, error) {
	var source parametersXML
	decoder := xml.NewDecoder(reader)
	if err := decoder.Decode(&source); err != nil {
		return config.Config{}, nil, fmt.Errorf("decode Parameters.xml: %w", err)
	}

	result := config.Default()
	result.Logging.Level = config.LogLevelError
	var warnings []string
	warnings = appendUnknownAttributes(warnings, "Parameters", source.OtherAttributes)

	version, err := parseRequiredInt("Parameters@Version", source.Version)
	if err != nil {
		return config.Config{}, warnings, err
	}
	if version > config.CurrentVersion {
		return config.Config{}, warnings, fmt.Errorf(
			"parameters version %d is newer than supported version %d",
			version,
			config.CurrentVersion,
		)
	}

	if value, present, err := parseOptionalLegacyBool("Parameters@IsVerboseLog", source.IsVerboseLog); err != nil {
		return config.Config{}, warnings, err
	} else if present && value {
		result.Logging.Level = config.LogLevelInfo
	}
	if value, present, err := parseOptionalLegacyBool("Parameters@IsDriverLog", source.IsDriverLog); err != nil {
		return config.Config{}, warnings, err
	} else if present {
		result.Logging.DriverTrace = value
	}
	if value, present, err := parseOptionalLegacyBool("Parameters@IsDriverLog2", source.IsDriverLog2); err != nil {
		return config.Config{}, warnings, err
	} else if present {
		result.Logging.DriverTraceRaw = value
	}
	if value, present, err := parseOptionalLegacyBool("Parameters@ClearOldLogs", source.ClearOldLogs); err != nil {
		return config.Config{}, warnings, err
	} else if present {
		result.Runtime.ClearOldLogs = value
	}

	result.Plants = make([]config.Plant, 0, len(source.Plants))
	for index, sourcePlant := range source.Plants {
		plant, plantWarnings, err := importPlant(index, sourcePlant)
		warnings = append(warnings, plantWarnings...)
		if err != nil {
			return config.Config{}, warnings, err
		}
		result.Plants = append(result.Plants, plant)
	}

	return result, warnings, nil
}

func importPlant(index int, source plantXML) (config.Plant, []string, error) {
	path := fmt.Sprintf("Plant[%d]", index)
	result := config.DefaultPlant()
	var warnings []string
	warnings = appendUnknownAttributes(warnings, path, source.OtherAttributes)

	version, err := parseRequiredInt(path+"@Version", source.Version)
	if err != nil {
		return config.Plant{}, warnings, err
	}
	if version > config.CurrentVersion {
		return config.Plant{}, warnings, fmt.Errorf(
			"%s version %d is newer than supported version %d",
			path,
			version,
			config.CurrentVersion,
		)
	}

	if source.Number != "" {
		result.Number, err = parseInt(path+"@Number", source.Number)
		if err != nil {
			return config.Plant{}, warnings, err
		}
	}
	result.Name = source.Name

	driverNumber := 0
	if source.DriverNo != "" {
		driverNumber, err = parseInt(path+"@DriverNo", source.DriverNo)
		if err != nil {
			return config.Plant{}, warnings, err
		}
	}
	result.Driver, err = config.DriverTypeFromLegacy(driverNumber)
	if err != nil {
		return config.Plant{}, warnings, fmt.Errorf("%s@DriverNo: %w", path, err)
	}

	if source.IsDisabled != "" {
		disabled, err := parseInt(path+"@IsDisabled", source.IsDisabled)
		if err != nil {
			return config.Plant{}, warnings, err
		}
		result.Enabled = disabled == 0
	}
	result.Address = source.AddressIP
	if source.PortNo != "" {
		result.Port, err = parseInt(path+"@PortNo", source.PortNo)
		if err != nil {
			return config.Plant{}, warnings, err
		}
	}
	if source.SerialNumber != "" {
		result.Serial, err = parseInt64(path+"@SerialNumber", source.SerialNumber)
		if err != nil {
			return config.Plant{}, warnings, err
		}
	}

	result.Cloud.PlantID = firstNonEmpty(source.LegacyPlantID, source.PlantID)
	result.Cloud.PlantToken = firstNonEmpty(source.LegacyPlantToken, source.PlantToken)
	result.Cloud.MQTTAddress = firstNonEmpty(source.LegacyMQTTAddress, source.MQTTAddress)
	if result.Cloud.MQTTAddress == "" {
		result.Cloud.MQTTAddress = config.DefaultCloud().MQTTAddress
	}
	mqttPort := firstNonEmpty(source.LegacyMQTTPort, source.MQTTPort)
	if mqttPort != "" {
		result.Cloud.MQTTPort, err = parseInt(path+"@GbbOptimizer_Mqtt_Port", mqttPort)
		if err != nil {
			return config.Plant{}, warnings, err
		}
	}

	result.SubInverters = make([]config.SubInverter, 0, len(source.SubInverters))
	for subIndex, sourceSubInverter := range source.SubInverters {
		subInverter, subWarnings, err := importSubInverter(index, subIndex, sourceSubInverter)
		warnings = append(warnings, subWarnings...)
		if err != nil {
			return config.Plant{}, warnings, err
		}
		result.SubInverters = append(result.SubInverters, subInverter)
	}

	return result, warnings, nil
}

func importSubInverter(
	plantIndex int,
	index int,
	source subInverterXML,
) (config.SubInverter, []string, error) {
	path := fmt.Sprintf("Plant[%d].SubInverter[%d]", plantIndex, index)
	result := config.DefaultSubInverter()
	var warnings []string
	warnings = appendUnknownAttributes(warnings, path, source.OtherAttributes)

	version, err := parseRequiredInt(path+"@Version", source.Version)
	if err != nil {
		return config.SubInverter{}, warnings, err
	}
	if version > config.CurrentVersion {
		return config.SubInverter{}, warnings, fmt.Errorf(
			"%s version %d is newer than supported version %d",
			path,
			version,
			config.CurrentVersion,
		)
	}

	result.Address = source.AddressIP
	if source.PortNo != "" {
		result.Port, err = parseInt(path+"@PortNo", source.PortNo)
		if err != nil {
			return config.SubInverter{}, warnings, err
		}
	}
	if source.SerialNumber != "" {
		result.Serial, err = parseInt64(path+"@SerialNumber", source.SerialNumber)
		if err != nil {
			return config.SubInverter{}, warnings, err
		}
	}
	if source.DongleSerialNumber != "" {
		result.DongleSerial, err = parseInt64(path+"@DongleSerialNumber", source.DongleSerialNumber)
		if err != nil {
			return config.SubInverter{}, warnings, err
		}
	}

	return result, warnings, nil
}

func appendUnknownAttributes(warnings []string, path string, attrs []xml.Attr) []string {
	for _, attr := range attrs {
		warnings = append(warnings, fmt.Sprintf(
			"%s has unknown attribute %q",
			path,
			attr.Name.Local,
		))
	}
	return warnings
}

func parseRequiredInt(path, value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is required", path)
	}
	return parseInt(path, value)
}

func parseInt(path, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %q", path, value)
	}
	return parsed, nil
}

func parseInt64(path, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %q", path, value)
	}
	return parsed, nil
}

func parseOptionalLegacyBool(path, value string) (bool, bool, error) {
	if value == "" {
		return false, false, nil
	}
	switch strings.ToLower(value) {
	case "0", "false":
		return false, true, nil
	case "1", "true":
		return true, true, nil
	default:
		return false, true, fmt.Errorf("%s must be 0 or 1: %q", path, value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type parametersXML struct {
	XMLName         xml.Name   `xml:"Parameters"`
	Version         string     `xml:"Version,attr"`
	ServerAutoStart string     `xml:"Server_AutoStart,attr"`
	IsVerboseLog    string     `xml:"IsVerboseLog,attr"`
	IsDriverLog     string     `xml:"IsDriverLog,attr"`
	IsDriverLog2    string     `xml:"IsDriverLog2,attr"`
	ClearOldLogs    string     `xml:"ClearOldLogs,attr"`
	Plants          []plantXML `xml:"Plant"`
	OtherAttributes []xml.Attr `xml:",any,attr"`
}

type plantXML struct {
	Version           string           `xml:"Version,attr"`
	Number            string           `xml:"Number,attr"`
	Name              string           `xml:"Name,attr"`
	DriverNo          string           `xml:"DriverNo,attr"`
	IsDisabled        string           `xml:"IsDisabled,attr"`
	AddressIP         string           `xml:"AddressIP,attr"`
	PortNo            string           `xml:"PortNo,attr"`
	SerialNumber      string           `xml:"SerialNumber,attr"`
	PlantID           string           `xml:"GbbOptimizer_PlantId,attr"`
	PlantToken        string           `xml:"GbbOptimizer_PlantToken,attr"`
	MQTTAddress       string           `xml:"GbbOptimizer_Mqtt_Address,attr"`
	MQTTPort          string           `xml:"GbbOptimizer_Mqtt_Port,attr"`
	LegacyPlantID     string           `xml:"GbbVictronWeb_PlantId,attr"`
	LegacyPlantToken  string           `xml:"GbbVictronWeb_PlantToken,attr"`
	LegacyMQTTAddress string           `xml:"GbbVictronWeb_Mqtt_Address,attr"`
	LegacyMQTTPort    string           `xml:"GbbVictronWeb_Mqtt_Port,attr"`
	SubInverters      []subInverterXML `xml:"SubInverter"`
	OtherAttributes   []xml.Attr       `xml:",any,attr"`
}

type subInverterXML struct {
	Version            string     `xml:"Version,attr"`
	AddressIP          string     `xml:"AddressIP,attr"`
	PortNo             string     `xml:"PortNo,attr"`
	SerialNumber       string     `xml:"SerialNumber,attr"`
	DongleSerialNumber string     `xml:"DongleSerialNumber,attr"`
	OtherAttributes    []xml.Attr `xml:",any,attr"`
}
