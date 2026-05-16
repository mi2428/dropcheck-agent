package io.dropcheck.agent

import android.app.admin.DeviceAdminReceiver as AndroidDeviceAdminReceiver

/** Marker receiver used when the APK is installed as a device-owner/admin agent. */
class DeviceAdminReceiver : AndroidDeviceAdminReceiver()

/** Legacy device-owner component kept so previously provisioned test devices remain manageable. */
class DropDeviceAdminReceiver : AndroidDeviceAdminReceiver()
