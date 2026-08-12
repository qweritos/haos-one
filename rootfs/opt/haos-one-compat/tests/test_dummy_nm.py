import json
import time

from haos_one_compat.dummy_nm import build_state, load_network_projection


def test_projection_uses_active_haoswg0(tmp_path):
    path = tmp_path / "network.json"
    path.write_text(
        json.dumps(
            {
                "version": 1,
                "interface": "haoswg0",
                "address": "10.203.0.2",
                "prefix": 30,
                "gateway": "10.203.0.1",
                "nameservers": ["192.168.65.7"],
                "updated_unix": int(time.time()),
            }
        ),
        encoding="utf-8",
    )
    projection = load_network_projection(str(path))
    state = build_state(projection)
    device = next(iter(state.devices.values()))
    ip4 = next(iter(state.ip4_configs.values()))
    assert device.interface == "haoswg0"
    assert device.device_type == 1
    assert device.driver == "dummy-eth"
    assert ip4.address_data[0]["address"].value == "10.203.0.2"
    assert ip4.gateway == "10.203.0.1"
    assert ip4.nameserver_data[0]["address"].value == "192.168.65.7"


def test_projection_falls_back_when_stale_or_absent(tmp_path):
    path = tmp_path / "network.json"
    assert load_network_projection(str(path)) is None
    path.write_text(
        json.dumps(
            {
                "version": 1,
                "interface": "haoswg0",
                "address": "10.203.0.2",
                "prefix": 30,
                "gateway": "10.203.0.1",
                "updated_unix": int(time.time()) - 60,
            }
        ),
        encoding="utf-8",
    )
    assert load_network_projection(str(path)) is None
    device = next(iter(build_state().devices.values()))
    assert device.interface == "eth0"
