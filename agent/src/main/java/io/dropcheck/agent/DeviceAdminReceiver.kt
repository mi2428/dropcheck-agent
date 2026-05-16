package io.dropcheck.agent

import android.app.admin.DeviceAdminReceiver as AndroidDeviceAdminReceiver

/** Marker receiver used when the APK is installed as a device-owner/admin agent. */
class DeviceAdminReceiver : AndroidDeviceAdminReceiver()

/**
 * Legacy device-owner component kept for handsets provisioned before the
 * receiver was renamed. New setups should use DeviceAdminReceiver, but existing
 * owners must keep this class installed until they are removed or reset.
 */
class DropDeviceAdminReceiver : AndroidDeviceAdminReceiver()
