# Config Device-Plugin

We provide a configuration file for the device-plugin, The configuration is a configmap.

The configuration file is a json file, and the following is an example of the configuration file:
ddd
```json
    {
        "nodeconfig": [
            {
                "name": "m5-cloudinfra-online02",
                "devicememoryscaling": 1.8,
                "devicesplitcount": 10,
                "migstrategy":"none",
                "filterdevices": {
                  "uuid": [],
                  "index": []
                }
            }
        ]
    }
```

- `name`: The name of the node.
- `devicememoryscaling`: The memory scaling factor for the device of the node. The memory scaling factor is used to calculate the memory size of the device. The memory size of the device is calculated by the following formula: `memory size = memory scaling factor * device memory size`.
- `devicesplitcount`: The number of device instances that the device is split into of the node. The device instances are used to create the device resources in the device-plugin.
- `migstrategy`: The MIG strategy for the device instances. The MIG strategy can be one of the following values:
  - `none`: The device is disabled for MIG.
  - `single`: The device is enabled for MIG with a single instance.
  - `mixed`: The device is enabled for MIG with multiple instances.
- `filterdevices`: The filter devices for the device instances. The filter devices can be one of the following values:
  - `uuid`: The UUID of the device.
  - `index`: The index of the device.