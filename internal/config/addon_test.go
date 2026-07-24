package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestHomeAssistantAddonOptionsMatchConfigurationSchema(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../deploy/addon/config.yaml")
	if err != nil {
		t.Fatalf("read add-on config: %v", err)
	}
	var manifest map[string]any
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse add-on config: %v", err)
	}

	arches := stringSlice(t, manifest["arch"], "arch")
	if !slices.Equal(arches, []string{"aarch64", "amd64", "armv7"}) {
		t.Fatalf("add-on architectures = %v", arches)
	}

	options := canonicalAddonOptions(t, stringMap(t, manifest["options"], "options"))
	if err := ValidateSchema(options); err != nil {
		t.Fatalf("default add-on options fail JSON Schema: %v", err)
	}
	encoded, err := yaml.Marshal(options)
	if err != nil {
		t.Fatalf("encode default add-on options: %v", err)
	}
	var configuration Config
	if err := yaml.Unmarshal(encoded, &configuration); err != nil {
		t.Fatalf("decode default add-on options: %v", err)
	}
	if err := Validate(configuration); err != nil {
		t.Fatalf("default add-on options fail Go validation: %v", err)
	}

	schema := stringMap(t, manifest["schema"], "schema")
	assertAddonEnum(
		t,
		schema,
		[]string{"plants", "[]", "driver"},
		stringsFrom(DriverTypes()),
	)
	assertAddonEnum(
		t,
		schema,
		[]string{"logging", "level"},
		stringsFrom(LogLevels()),
	)
	assertAddonEnum(
		t,
		schema,
		[]string{"plants", "[]", "serial_parity"},
		stringsFrom(Parities()),
	)
	assertFlatAddonItem(t, schema, "plants")
	assertFlatAddonItem(t, schema, "sub_inverters")
}

func assertAddonEnum(
	t *testing.T,
	root map[string]any,
	path []string,
	want []string,
) {
	t.Helper()
	var current any = root
	for _, part := range path {
		if part == "[]" {
			list, ok := current.([]any)
			if !ok || len(list) != 1 {
				t.Fatalf("add-on schema path %v is not a one-item list", path)
			}
			current = list[0]
			continue
		}
		current = stringMap(t, current, strings.Join(path, "."))[part]
	}
	value, ok := current.(string)
	if !ok {
		t.Fatalf("add-on schema path %v = %#v, want string", path, current)
	}
	const prefix = "list("
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, ")") {
		t.Fatalf("add-on schema enum %v = %q", path, value)
	}
	got := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, prefix), ")"), "|")
	if !slices.Equal(got, want) {
		t.Fatalf("add-on schema enum %v = %v, want %v", path, got, want)
	}
}

func stringMap(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want mapping", path, value)
	}
	return result
}

func stringSlice(t *testing.T, value any, path string) []string {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T, want list", path, value)
	}
	result := make([]string, len(values))
	for index, item := range values {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("%s[%d] = %T, want string", path, index, item)
		}
		result[index] = text
	}
	return result
}

func stringsFrom[T ~string](values []T) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func assertFlatAddonItem(t *testing.T, schema map[string]any, name string) {
	t.Helper()
	items, ok := schema[name].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("add-on schema %s is not a one-item list", name)
	}
	item := stringMap(t, items[0], "schema."+name+"[]")
	for field, value := range item {
		if _, ok := value.(string); !ok {
			t.Fatalf(
				"schema.%s[].%s = %T; nested collections exceed HA UI depth",
				name,
				field,
				value,
			)
		}
	}
}

func canonicalAddonOptions(t *testing.T, options map[string]any) map[string]any {
	t.Helper()
	subInverters, ok := options["sub_inverters"].([]any)
	if !ok {
		t.Fatal("options.sub_inverters is not a list")
	}
	rawPlants, ok := options["plants"].([]any)
	if !ok {
		t.Fatal("options.plants is not a list")
	}

	plants := make([]any, 0, len(rawPlants))
	for index, value := range rawPlants {
		plant := stringMap(t, value, fmt.Sprintf("options.plants[%d]", index))
		number := plant["number"]
		routed := make([]any, 0)
		for subIndex, rawSub := range subInverters {
			sub := stringMap(
				t,
				rawSub,
				fmt.Sprintf("options.sub_inverters[%d]", subIndex),
			)
			if sub["plant_number"] != number {
				continue
			}
			routed = append(routed, map[string]any{
				"serial":        sub["serial"],
				"dongle_serial": sub["dongle_serial"],
				"address":       sub["address"],
				"port":          sub["port"],
			})
		}
		plants = append(plants, map[string]any{
			"number":  number,
			"name":    plant["name"],
			"enabled": plant["enabled"],
			"driver":  plant["driver"],
			"address": plant["address"],
			"port":    plant["port"],
			"serial":  plant["serial"],
			"serial_port": map[string]any{
				"device":    plant["serial_device"],
				"baud":      plant["serial_baud"],
				"data_bits": plant["serial_data_bits"],
				"parity":    plant["serial_parity"],
				"stop_bits": plant["serial_stop_bits"],
			},
			"cloud": map[string]any{
				"plant_id":                 plant["cloud_plant_id"],
				"plant_token":              plant["cloud_plant_token"],
				"mqtt_address":             plant["cloud_mqtt_address"],
				"mqtt_port":                plant["cloud_mqtt_port"],
				"tls_insecure_skip_verify": plant["cloud_tls_insecure_skip_verify"],
			},
			"sub_inverters": routed,
		})
	}
	return map[string]any{
		"version": options["version"],
		"runtime": options["runtime"],
		"logging": options["logging"],
		"plants":  plants,
	}
}
