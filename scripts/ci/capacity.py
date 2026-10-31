"""Phase admission and sampled filesystem reserve for verification."""
import json
import os
from pathlib import Path
import time

GIB = 1024**3
RESERVE = 2*GIB
MEASURED_BUILD = 6845239296
COMPRESSED_PARENTS = 325060003
RESIDENT_IMAGES = 3437273088
COLD_ALLOWANCE = GIB
NATIVE_WORK = 5*GIB//2
PG_DATA = 2*GIB
PG_IMAGE = 3*GIB//4
CONTAINER_WRITES = GIB//4
CONTEXT = GIB//8
BOUND_LOCK = '0bf1afc4924dcdcb68ea98170d60f2ac30fc356fc3b6de757119633f1054af4d'
BOUND_CI_DOCKERFILE = '27dacfd94f342843c8d1e79a9d1848b1e0ac37f39254e8728b7353f2ba4c4a49'


class CapacityError(RuntimeError):
    pass


def verify_contract(lock_sha, ci_dockerfile_sha):
    if (lock_sha, ci_dockerfile_sha) != (BOUND_LOCK, BOUND_CI_DOCKERFILE):
        raise CapacityError('runtime inputs changed; capacity contract requires review')


def filesystem(path):
    path = Path(path).absolute()
    while True:
        try:
            metadata = path.stat()
            vfs = os.statvfs(path)
            return {'device': str(metadata.st_dev), 'path': str(path),
                    'free': vfs.f_bavail*vfs.f_frsize,
                    'used': (vfs.f_blocks-vfs.f_bfree)*vfs.f_frsize}
        except FileNotFoundError:
            if path.parent == path:
                raise CapacityError('filesystem measurement has no existing ancestor')
            path = path.parent
        except OSError as error:
            raise CapacityError('filesystem measurement failed: '+type(error).__name__+' at '+str(path)) from error


def groups(paths, allowances):
    result = {}
    for role, path in paths.items():
        value = filesystem(path)
        group = result.setdefault(value['device'], dict(value, roles=[], allowance=0))
        group['free'] = min(group['free'], value['free'])
        group['used'] = max(group['used'], value['used'])
        group['roles'].append(role)
        group['allowance'] += allowances.get(role, 0)
    return result


def phases(prepared):
    native = {'work': NATIVE_WORK, 'pg': PG_DATA,
              'docker': CONTAINER_WRITES + (0 if prepared else PG_IMAGE)}
    if prepared:
        return {'native': native}
    return {
        'build': {'docker': MEASURED_BUILD+COMPRESSED_PARENTS+COLD_ALLOWANCE, 'work': CONTEXT},
        'cold-native': dict(native, docker=native['docker']+RESIDENT_IMAGES),
    }


def admission(paths, plans):
    observations, required = {}, {}
    for phase, allowances in plans.items():
        observations[phase] = groups(paths, allowances)
        for device, value in observations[phase].items():
            required[device] = max(required.get(device, 0), value['allowance']+RESERVE)
    for phase in observations.values():
        for device, value in phase.items():
            if value['free'] < required[device]:
                raise CapacityError('capacity admission failed on device '+device+': free '+str(value['free'])+' < required '+str(required[device]))
    return {'phases': observations, 'required_free_by_device': required,
            'reserve_bytes': RESERVE, 'hosted_execution_observed': False}


class SpaceGuard:
    def __init__(self, paths, reports, name, allowances=None):
        self.paths, self.allowances = dict(paths), allowances
        self.log = Path(reports)/(name+'.capacity.jsonl')
        self.summary = Path(reports)/(name+'.capacity.json')
        self.initial, self.minimum_free, self.peak_growth = {}, {}, {}
        self.samples, self.failure = 0, None
        self.last = 0
        if self.log.exists() or self.summary.exists():
            raise ValueError('refusing to replace capacity evidence')

    def check(self, force=False):
        if self.failure:
            raise CapacityError(self.failure)
        if not force and time.monotonic()-self.last < 0.5:
            return
        self.last = time.monotonic()
        try:
            current = groups(self.paths, self.allowances or {})
            if self.initial and (set(current) != set(self.initial) or
                                 any(current[d]['roles'] != self.initial[d]['roles'] for d in current)):
                raise CapacityError('filesystem identity changed during verification')
            if not self.initial:
                if self.allowances is not None:
                    admission(self.paths, {'current': self.allowances})
                self.initial = current
            self.samples += 1
            for device, value in current.items():
                growth = max(0, value['used']-self.initial[device]['used'])
                value['growth'] = growth
                self.minimum_free[device] = min(self.minimum_free.get(device, value['free']), value['free'])
                self.peak_growth[device] = max(self.peak_growth.get(device, 0), growth)
            with self.log.open('a') as output:
                output.write(json.dumps({'monotonic': self.last, 'filesystems': current})+'\n')
            for device, value in current.items():
                if value['free'] < RESERVE:
                    raise CapacityError('2 GiB free-space reserve crossed on device '+device)
                if self.allowances is not None and value['growth'] > value['allowance']:
                    raise CapacityError('phase growth allowance crossed on device '+device)
        except (OSError, CapacityError) as error:
            self.failure = str(error)
            self.finish()
            raise CapacityError(self.failure) from error

    def finish(self):
        self.summary.write_text(json.dumps({
            'paths': {k: str(v) for k, v in self.paths.items()},
            'allowances': self.allowances, 'reserve_bytes': RESERVE,
            'samples': self.samples, 'minimum_free': self.minimum_free,
            'maximum_sampled_net_growth': self.peak_growth, 'failure': self.failure,
            'scope': 'VFS device occupancy, including unrelated allocations. Sampled every 0.5 seconds during commands; not a quota or an owner-specific inode measurement.',
        }, indent=2)+'\n')
