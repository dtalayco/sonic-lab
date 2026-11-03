# Topology and schema notes

Draft. What the Homa lab needed that the topology description can't
express today, and a possible direction. Many of these apply to any
lab with hosts, not just Homa.

## Today

tgen writes one `topology.yml`, but only webmesh reads it, and webmesh
uses only `links`:

```yaml
topology:
  name: spine-leaf-1x1
  switches:
    spine1: {role: spine, asn: 65000, ports: {Ethernet0: {}, Ethernet4: {}}}
  hosts:
    host1: {ports: {Ethernet0: {}}}
  links:
    - endpoints:
        - {device: spine1-linux, port: Ethernet4}
        - {device: host1, port: Ethernet0}
```

- **Mixed identity:** switches are keyed by logical name (`spine1`), but
  link endpoints name the backing container (`spine1-linux`), which is
  an implementation detail of how a SONiC switch is run. Hosts are named
  by container.
- **Switch config lives elsewhere:** `role` and `asn` are informational.
  The real config (BGP, loopbacks, SRv6) comes from tgen's flags and code,
  rendered into `sonic_entry.sh` and `frr/*.conf`, and is routing only.
- **Hosts are just ports:** there's no type, image, addresses or interface
  settings, and the compose file hardcodes one host image.
- **Shapes only:** tgen generates from flags, so a topology outside its
  shorthands (here, one switch) must be written by hand.

## What the Homa lab needed

| Need | How it was done | Where it belongs |
|---|---|---|
| One switch, four hosts | Hand-written compose and topology | Topology |
| Host image with Homa tools | Custom `Dockerfile.host`, tools mounted from the VM | Device type |
| Host addresses and names | `host_entry.sh` and `extra_hosts` | Device config, or an address plan |
| TX checksum offload off on hosts | `host_entry.sh` | Device type default |
| L2 only: untagged VLAN 10 on host ports, no routing | Custom `sonic_entry.sh` | Profile |
| Kernel 6.17 and the Homa module | `setup-vm.sh`, `build-homa.sh` | Lab requirements, checked before start |
| Rate limits, queues, DSCP bands | `bottleneck.sh` | Optional simulation mode |

## Direction: separate the layers

1. **Connectivity:** devices with a name and a type, and links between
   logical ports. This is all webmesh needs. How a device maps to
   containers (one for a host, two for SONiC VS) belongs to its type, not
   to the links.
2. **Device types:** image, containers, port naming, defaults such as
   interface settings, and how config is applied. `sonic-vs` and
   `linux-host` first; `ovs` or `linux-bridge` later.
3. **Device config:** per-device settings such as addresses.
4. **Profiles:** named, reusable config applied to many devices, such as
   an L2 access VLAN or an L3 routing setup. These are the matrix axes in
   the testing doc.
5. **Requirements:** what the machine must provide (kernel version,
   modules), so a lab can fail early with a clear message.
6. **Simulation mode:** optional link model (rate, queue size,
   scheduler), off by default.

A sketch, not a proposal for exact syntax:

```yaml
version: 1
name: homa-1x4

devices:
  sw1:   {type: sonic-vs, profiles: [l2-access]}
  host1: {type: linux-host, image: homa-host, address: 10.70.2.1/24}
  host2: {type: linux-host, image: homa-host, address: 10.70.2.2/24}

links:
  - [sw1:Ethernet0, host1:eth1]
  - [sw1:Ethernet4, host2:eth1]

profiles:
  l2-access:
    vlans:
      10: {untagged: [Ethernet0, Ethernet4]}

requirements:
  kernel: ">= 6.17"
  modules: [homa]

simulation:
  links:
    default: {rate: 2gbit, queue: 2mb, scheduler: dscp-8-band}
```

## Open questions

- One file, or connectivity separate from config? Separate files would
  let the same wiring run under different profiles, as the test matrix
  needs.
- Addresses: listed per device, or assigned from a plan (subnet per VLAN,
  host N gets .N)?
- Port names: SONiC's `EthernetN` and a host's `ethN` are both type
  specific. Should links use the type's names, or neutral ones that each
  type maps?
- Who renders config per device type: tgen, or plugins that labctl calls?
- How does this relate to the per-lab `lab` field proposed for webmesh,
  and to container name prefixes?
- Should hosts still be attached to Docker's network? It gives them a
  second path out that hides lab wiring mistakes.
