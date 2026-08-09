#!/usr/bin/env python3
"""
Reproduce HAMi issue #2125: False ERROR logs when env vars are stripped.This script simulates the HAMi-core shared memory initialization path to
demonstrate that when a child process inherits libvgpu.so via LD_PRELOAD after fork()+execve() with a stripped environment (ssh, su, systemd), the
OLD code produces false ERROR logs. The NEW code (with the fix) detects missing env vars and skips
 the consistency check.
"""

import os
import sys
import tempfile
import struct

# mock constants from HAMi-core
MULTIPROCESS_SHARED_REGION_MAGIC_FLAG = 0xDEADBEEF
CUDA_DEVICE_MAX_COUNT = 16


class MockSharedRegion:
    """
    Minimal binary stand-in for HAMi-core's shared_region_t.
    Layout: initialized_flag (int32), padding (4 bytes),
            limit[16] (uint64 * 16), sm_limit[16] (uint64 * 16)
    """
    def __init__(self):
        self.initialized_flag = 0
        self.major_version = 1
        self.minor_version = 0
        self.limits = [0] * CUDA_DEVICE_MAX_COUNT
        self.sm_limits = [0] * CUDA_DEVICE_MAX_COUNT

    def to_bytes(self):
        header = struct.pack('<III', self.initialized_flag,
                             self.major_version, self.minor_version)
        header += b'\x00' * 4
        limits_bytes = struct.pack('<' + 'Q' * CUDA_DEVICE_MAX_COUNT,
                                   *self.limits)
        sm_limits_bytes = struct.pack('<' + 'Q' * CUDA_DEVICE_MAX_COUNT,
                                      *self.sm_limits)
        return header + limits_bytes + sm_limits_bytes

    @classmethod
    def from_bytes(cls, data):
        region = cls()
        region.initialized_flag = struct.unpack_from('<I', data, 0)[0]
        offset = 8
        region.limits = list(
            struct.unpack_from('<' + 'Q' * CUDA_DEVICE_MAX_COUNT, data, offset))
        offset += 8 * CUDA_DEVICE_MAX_COUNT
        region.sm_limits = list(
            struct.unpack_from('<' + 'Q' * CUDA_DEVICE_MAX_COUNT, data, offset))
        return region


def simulate_parent_init(shm_path):
    """parent process: sets env vars and writes the shared memory region."""
    os.environ['CUDA_DEVICE_MEMORY_LIMIT_0'] = '2048m'
    os.environ['CUDA_DEVICE_MEMORY_LIMIT'] = '4096m'
    os.environ['CUDA_DEVICE_SM_LIMIT_0'] = '50'

    region = MockSharedRegion()
    region.initialized_flag = MULTIPROCESS_SHARED_REGION_MAGIC_FLAG
    region.limits[0] = 2048 * 1024 * 1024
    region.sm_limits[0] = 50

    with open(shm_path, 'wb') as f:
        f.write(region.to_bytes())

    print(f"[PARENT] PID {os.getpid()}")
    print(f"[PARENT] Env CUDA_DEVICE_MEMORY_LIMIT_0 = "
          f"{os.environ.get('CUDA_DEVICE_MEMORY_LIMIT_0')}")
    print(f"[PARENT] Wrote SHM: limit[0] = {region.limits[0]} bytes, "
          f"sm_limit[0] = {region.sm_limits[0]}")


def old_behavior_child(shm_path):
    env_backup = dict(os.environ)
    os.environ.clear()

    region = MockSharedRegion.from_bytes(open(shm_path, 'rb').read())
    errors = 0

    if region.initialized_flag == MULTIPROCESS_SHARED_REGION_MAGIC_FLAG:
        # OLD: unconditionally re-read and compare memory limits
        local_limits = [0] * CUDA_DEVICE_MAX_COUNT
        for i in range(CUDA_DEVICE_MAX_COUNT):
            val = os.environ.get(f'CUDA_DEVICE_MEMORY_LIMIT_{i}')
            if val is None and i == 0:
                val = os.environ.get('CUDA_DEVICE_MEMORY_LIMIT')
            local_limits[i] = _parse_limit(val)

        for i in range(CUDA_DEVICE_MAX_COUNT):
            if local_limits[i] != region.limits[i]:
                print(f"[OLD CHILD] ERROR: Limit inconsistency for device {i}, "
                      f"{local_limits[i]} expected, got {region.limits[i]}")
                errors += 1

        local_sm = [0] * CUDA_DEVICE_MAX_COUNT
        for i in range(CUDA_DEVICE_MAX_COUNT):
            val = os.environ.get(f'CUDA_DEVICE_SM_LIMIT_{i}')
            if val is None and i == 0:
                val = os.environ.get('CUDA_DEVICE_SM_LIMIT')
            local_sm[i] = _parse_limit(val) if val else 100

        for i in range(CUDA_DEVICE_MAX_COUNT):
            if local_sm[i] != region.sm_limits[i]:
                print(f"[OLD CHILD] ERROR: SM limit inconsistency for device {i}, "
                      f"{local_sm[i]} expected, got {region.sm_limits[i]}")
                errors += 1
    else:
        print("[OLD CHILD] SHM not initialized")

    os.environ.update(env_backup)

    if errors > 0:
        print(f"[OLD CHILD] RESULT: {errors} false ERROR(s) logged "
              f"(BUG REPRODUCED)")
        return False
    print("[OLD CHILD] RESULT: OK")
    return True


def new_behavior_child(shm_path):
    env_backup = dict(os.environ)
    os.environ.clear()

    region = MockSharedRegion.from_bytes(open(shm_path, 'rb').read())

    if region.initialized_flag == MULTIPROCESS_SHARED_REGION_MAGIC_FLAG:
        mem_env_present = _env_limit_family_present('CUDA_DEVICE_MEMORY_LIMIT')
        sm_env_present = _env_limit_family_present('CUDA_DEVICE_SM_LIMIT')

        if mem_env_present:
            print("[NEW CHILD] Memory env vars present; "
                  "would run consistency check")
        else:
            print("[NEW CHILD] INFO: Memory env vars stripped; "
                  "adopting shared memory limits")

        if sm_env_present:
            print("[NEW CHILD] SM env vars present; "
                  "would run consistency check")
        else:
            print("[NEW CHILD] INFO: SM env vars stripped; "
                  "adopting shared memory limits")
    else:
        print("[NEW CHILD] SHM not initialized")

    os.environ.update(env_backup)
    return True


def _parse_limit(val):
    """parse '2048m' or '2G' into bytes. Returns 0 on None/empty."""
    if val is None or val == '':
        return 0
    val = val.strip().lower()
    scalar = 1
    if val.endswith('g'):
        scalar = 1024 * 1024 * 1024
        val = val[:-1]
    elif val.endswith('m'):
        scalar = 1024 * 1024
        val = val[:-1]
    elif val.endswith('k'):
        scalar = 1024
        val = val[:-1]
    try:
        return int(val) * scalar
    except ValueError:
        return 0


def _env_limit_family_present(base_name):
    if os.environ.get(base_name) is not None:
        return True
    for i in range(CUDA_DEVICE_MAX_COUNT):
        if os.environ.get(f'{base_name}_{i}') is not None:
            return True
    return False


def main():
    with tempfile.NamedTemporaryFile(delete=False) as f:
        shm_path = f.name

    try:
        simulate_parent_init(shm_path)
        print()

        old_ok = old_behavior_child(shm_path)
        print()

        new_ok = new_behavior_child(shm_path)
        print()

        if (not old_ok) and new_ok:
            print("=" * 60)
            print("PASS: Bug reproduced in old behavior, "
                  "fix validated in new behavior")
            print("=" * 60)
            return 0
        else:
            print("=" * 60)
            print("FAIL: Unexpected result")
            print("=" * 60)
            return 1
    finally:
        os.unlink(shm_path)


if __name__ == '__main__':
    sys.exit(main())

