# TUS Uploader for VMWare vRA

A command-line tool to upload plugins or packages to vRA.

# Usage

To upload the Infoblox IPAM plugin

```
./tus-uploader --vra-username=administrator --vra-password=XXX Infoblox.zip https://vrahost/provisioning/ipam/api/providers/packages/import
```

# License

MIT or ASL-2.0.
