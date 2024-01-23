package dpm

// PluginNameList contains last names of discovered resources. This type is used by Discover
// implementation to inform manager about changes in found resources, e.g. last name of resource
// "color.example.com/red" is "red".
type PluginNameList []string

// ListerInterface serves as an interface between imlementation and Manager machinery. User passes
// implementation of this interface to NewManager function. Manager will use it to obtain resource
// namespace, monitor available resources and instantate a new plugin for them.
type ListerInterface interface {
	// GetResourceNamespace must return namespace (vendor ID) of implemented Lister. e.g. for
	// resources in format "color.example.com/<color>" that would be "color.example.com".
	GetResourceNamespace() string
	// Discover notifies manager with a list of currently available resources in its namespace.
	// e.g. if "color.example.com/red" and "color.example.com/blue" are available in the system,
	// it would pass PluginNameList{"red", "blue"} to given channel. In case list of
	// resources is static, it would use the channel only once and then return. In case the list is
	// dynamic, it could block and pass a new list each times resources changed. If blocking is
	// used, it should check whether the channel is closed, i.e. Discover should stop.
	Discover(chan PluginNameList)
	// NewPlugin instantiates a plugin implementation. It is given the last name of the resource,
	// e.g. for resource name "color.example.com/red" that would be "red". It must return valid
	// implementation of a PluginInterface.
	NewPlugin(string) PluginInterface
}
