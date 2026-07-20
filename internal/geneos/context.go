package geneos

var KnownVariables = []string{
	"_ALERT_TYPE",
	"_ALERT_TIME",
	"_COLUMN",
	"_DATAVIEW",
	"_GATEWAY",
	"_HOSTNAME",
	"_MANAGED_ENTITY",
	"_NETPROBE_HOST",
	"_PREVIOUS_SEV",
	"_PROBE",
	"_ROWNAME",
	"_RULE",
	"_SAMPLER",
	"_SAMPLER_GROUP",
	"_SEVERITY",
	"_VALUE",
	"_VARIABLE",
	"_VARIABLEPATH",
}

type Context struct {
	values map[string]string
}

func FromEnv(getenv func(string) string) Context {
	values := make(map[string]string, len(KnownVariables))
	for _, key := range KnownVariables {
		values[key] = getenv(key)
	}
	return Context{values: values}
}

func FromMap(values map[string]string) Context {
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return Context{values: copyValues}
}

func (c Context) Get(key string) string {
	return c.values[key]
}

func (c Context) Values() map[string]string {
	copyValues := make(map[string]string, len(c.values))
	for key, value := range c.values {
		copyValues[key] = value
	}
	return copyValues
}
