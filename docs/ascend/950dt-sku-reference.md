# Ascend950DT SKU reference

The HAMi chart's enabled Ascend950DT configuration targets the 9582 SKU as one
whole-card device: 98,304 MiB of HBM and 32 AI cores. This configuration was
tested on one Ascend950DT 9582 node. That result does not validate the other
SKUs listed below.

The following values are a reference from a supplied SKU comparison image.
They do not establish each SKU's reported DCMI identity, usable HBM, AI core
count, or device-plugin compatibility. Verify those details on the target
hardware before configuring another SKU.

| AI cores | HBM | SKUs in the comparison image |
|---:|---:|---|
| 36 | 144 GiB | DT_9591, DT_95A1 |
| 36 | 96 GiB | DT_9592, DT_9595, DT_95A2 |
| 36 | 64 GiB | DT_9596 |
| 32 | 144 GiB | DT_9581 |
| 32 | 126* GiB | DT_9583 |
| 32 | 96 GiB | DT_9582, DT_9582x, DT_9585 |
| 32 | 84 GiB | DT_9584, DT_9587 |
| 32 | 64 GiB | DT_9586 |
| 32 | 56 GiB | DT_9588 |
| 28 | 144 GiB | DT_9571 |
| 28 | 126* GiB | DT_9573 |
| 28 | 96 GiB | DT_9572, DT_9575 |
| 28 | 84 GiB | DT_9574, DT_9577 |
| 28 | 64 GiB | DT_9576 |
| 28 | 56 GiB | DT_9578 |
| 8 | 24 GiB | DT_950x |
| 8 | 16 GiB | DT_950y |

The image does not explain the asterisk on 126 GiB. Do not enable multiple
`devices.ascend.configs` entries with the same `chipName`: the scheduler cannot
distinguish these SKUs by that field. Replace the 9582 entry only after
validating the target SKU's actual identity and capacity.
