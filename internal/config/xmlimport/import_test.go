package xmlimport

import (
	"os"
	"strings"
	"testing"

	"github.com/KrzysztofHajdamowicz/gbbconnect-go/internal/config"
)

func TestImportOfficialSamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		subInverters int
	}{
		{name: "basic", path: "testdata/basic.xml"},
		{name: "sub-inverters", path: "testdata/sub-inverters.xml", subInverters: 1},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			imported, warnings := importFixture(t, test.path)
			if len(warnings) != 0 {
				t.Fatalf("warnings = %v, want none", warnings)
			}
			if err := config.Validate(imported); err != nil {
				t.Fatalf("imported config does not validate: %v", err)
			}

			plant := imported.Plants[0]
			if plant.Driver != config.DriverSolarmanV5 ||
				!plant.Enabled ||
				plant.Address != "1.2.3.4" ||
				plant.Port != 8899 ||
				plant.Serial != 123456 ||
				plant.Cloud.PlantID != "plant-id" ||
				plant.Cloud.PlantToken != "plant-token" {
				t.Fatalf("unexpected plant: %#v", imported.Redacted().Plants[0])
			}
			if len(plant.SubInverters) != test.subInverters {
				t.Fatalf("len(SubInverters) = %d, want %d", len(plant.SubInverters), test.subInverters)
			}
		})
	}
}

func TestImportLegacyAliasesAndUnknownAttributeWarnings(t *testing.T) {
	t.Parallel()

	imported, warnings := importFixture(t, "testdata/legacy-aliases.xml")
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want 2", warnings)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "UnexpectedParameter") ||
		!strings.Contains(strings.Join(warnings, "\n"), "UnexpectedPlant") {
		t.Fatalf("warnings do not identify unknown attributes: %v", warnings)
	}

	plant := imported.Plants[0]
	if plant.Driver != config.DriverModbusTCP ||
		plant.Enabled ||
		plant.Cloud.PlantID != "legacy-id" ||
		plant.Cloud.PlantToken != "legacy-token" ||
		plant.Cloud.MQTTAddress != "legacy-mqtt.example" ||
		plant.Cloud.MQTTPort != 8884 {
		t.Fatalf("legacy aliases were not imported: %#v", imported.Redacted().Plants[0])
	}
	if err := config.Validate(imported); err != nil {
		t.Fatalf("imported config does not validate: %v", err)
	}
}

func TestLegacyAliasesTakePrecedence(t *testing.T) {
	t.Parallel()

	const input = `<Parameters Version="1">
	  <Plant Version="1" Number="1" Name="Home" DriverNo="999" IsDisabled="0"
	    GbbVictronWeb_PlantId="legacy-id" GbbOptimizer_PlantId="new-id"
	    GbbVictronWeb_PlantToken="legacy-token" GbbOptimizer_PlantToken="new-token"/>
	</Parameters>`

	imported, _, err := Import(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if imported.Plants[0].Cloud.PlantID != "legacy-id" ||
		imported.Plants[0].Cloud.PlantToken != "legacy-token" {
		t.Fatalf("legacy aliases did not take precedence: %#v", imported.Redacted().Plants[0].Cloud)
	}
}

func TestImportErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "malformed XML", input: `<Parameters`, want: "decode Parameters.xml"},
		{name: "missing version", input: `<Parameters/>`, want: "Parameters@Version is required"},
		{name: "newer version", input: `<Parameters Version="2"/>`, want: "newer than supported"},
		{
			name: "unknown driver",
			input: `<Parameters Version="1">
			  <Plant Version="1" DriverNo="42"/>
			</Parameters>`,
			want: "unknown legacy driver number",
		},
		{
			name: "invalid integer",
			input: `<Parameters Version="1">
			  <Plant Version="1" PortNo="invalid"/>
			</Parameters>`,
			want: "PortNo must be an integer",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := Import(strings.NewReader(test.input))
			if err == nil {
				t.Fatal("Import() error = nil")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Import() error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func importFixture(t *testing.T, path string) (config.Config, []string) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	imported, warnings, importErr := Import(file)
	closeErr := file.Close()
	if importErr != nil {
		t.Fatalf("Import() error = %v", importErr)
	}
	if closeErr != nil {
		t.Fatalf("close fixture: %v", closeErr)
	}
	return imported, warnings
}
