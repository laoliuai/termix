-- Revert device_type and platform constraints to pre-web-ui values.
-- NOTE: this will fail if any rows with web/ios/windows values exist.
alter table devices
  drop constraint devices_device_type_check;

alter table devices
  add constraint devices_device_type_check
  check (device_type in ('host', 'android'));

alter table devices
  drop constraint devices_platform_check;

alter table devices
  add constraint devices_platform_check
  check (platform in ('macos', 'ubuntu', 'android'));
