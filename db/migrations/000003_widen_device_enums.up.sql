-- Widen device_type and platform check constraints to include web and ios clients.
alter table devices
  drop constraint devices_device_type_check;

alter table devices
  add constraint devices_device_type_check
  check (device_type in ('host', 'android', 'ios', 'web'));

alter table devices
  drop constraint devices_platform_check;

alter table devices
  add constraint devices_platform_check
  check (platform in ('macos', 'ubuntu', 'windows', 'android', 'ios', 'web'));
