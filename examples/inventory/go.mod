module example.com/inventory

go 1.24

require (
	github.com/turbolytics/temper v0.0.0-00010101000000-000000000000
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/turbolytics/temper => ../..
