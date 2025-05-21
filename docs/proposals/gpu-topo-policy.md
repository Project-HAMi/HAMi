# GPU topology scheduling strategy

## Use Case

### Node Select

If multiple nodes meet the requirements, the minimum meeting node should be selected first::
- `Node1`
```json
[
  {
    "uuid": "gpu0",
    "score": {
      "gpu1": "100",
      "gpu2": "100",
      "gpu3": "200"
    }
  },
  {
    "uuid": "gpu1",
    "score": {
      "gpu0": "100",
      "gpu2": "200",
      "gpu3": "100"
    }
  },
  {
    "uuid": "gpu2",
    "score": {
      "gpu0": "100",
      "gpu1": "200",
      "gpu3": "200"
    }
  },
  {
    "uuid": "gpu3",
    "score": {
      "gpu0": "200",
      "gpu1": "100",
      "gpu2": "200"
    }
  }
]
```


- `Node2`
```json
[
  {
    "uuid": "gpu0",
    "score": {
      "gpu1": "100",
      "gpu2": "100",
      "gpu3": "200",
      "gpu4": "200",
      "gpu5": "200"
    }
  },
  {
    "uuid": "gpu1",
    "score": {
      "gpu0": "100",
      "gpu2": "200",
      "gpu3": "100",
      "gpu4": "200",
      "gpu5": "200"
    }
  },
  {
    "uuid": "gpu2",
    "score": {
      "gpu0": "100",
      "gpu1": "200",
      "gpu3": "200",
      "gpu4": "200",
      "gpu5": "200"
    }
  },
  {
    "uuid": "gpu3",
    "score": {
      "gpu0": "200",
      "gpu1": "100",
      "gpu2": "200",
      "gpu4": "200",
      "gpu5": "200"
    }
  },
  {
    "uuid": "gpu4",
    "score": {
      "gpu0": "200",
      "gpu1": "100",
      "gpu2": "200",
      "gpu3": "200",
      "gpu5": "200"
    }
  },
  {
    "uuid": "gpu5",
    "score": {
      "gpu0": "200",
      "gpu1": "100",
      "gpu2": "200",
      "gpu3": "200",
      "gpu4": "200"
    }
  }
]
```

For example, the two nodes above should be given priority `Node1`.


### Device Select, One Pod One Device

1. When a Pod only needs one card, the card with the worst communication with other cards should be given priority if the video memory and computing power are sufficient, such as：
- `Node1`
```json
[
  {
    "uuid": "gpu0",
    "score": {
      "gpu1": "100",
      "gpu2": "100",
      "gpu3": "200"
    }
  },
  {
    "uuid": "gpu1",
    "score": {
      "gpu0": "100",
      "gpu2": "200",
      "gpu3": "100"
    }
  },
  {
    "uuid": "gpu2",
    "score": {
      "gpu0": "100",
      "gpu1": "200",
      "gpu3": "200"
    }
  },
  {
    "uuid": "gpu3",
    "score": {
      "gpu0": "200",
      "gpu1": "100",
      "gpu2": "200"
    }
  }
]
```
There are only four cards on this node, and `gpu0` and `gpu1` have the worst connectivity with other cards, so these two cards are preferred in single-card mode.

### Device Select, One Pod More Than One Device
1. When a Pod only needs multiple cards, if the video memory and computing power are sufficient, the card with the best communication with other cards should be given priority, such as：
- `Node1`
```json
[
  {
    "uuid": "gpu0",
    "score": {
      "gpu1": "100",
      "gpu2": "100",
      "gpu3": "200"
    }
  },
  {
    "uuid": "gpu1",
    "score": {
      "gpu0": "100",
      "gpu2": "200",
      "gpu3": "100"
    }
  },
  {
    "uuid": "gpu2",
    "score": {
      "gpu0": "100",
      "gpu1": "200",
      "gpu3": "200"
    }
  },
  {
    "uuid": "gpu3",
    "score": {
      "gpu0": "200",
      "gpu1": "100",
      "gpu2": "200"
    }
  }
]
```
There are only four cards on this node, and `gpu2` and `gpu3` have the best connectivity with other cards, so these two cards are given priority in multi-card mode.
