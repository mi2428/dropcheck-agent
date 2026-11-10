package io.dropcheck.agent

internal fun agentRuntimePermissions(sdk: Int): List<String> = buildList {
    add("android.permission.ACCESS_FINE_LOCATION")
    add("android.permission.ACCESS_COARSE_LOCATION")
    if (sdk >= 33) add("android.permission.NEARBY_WIFI_DEVICES")
    if (sdk >= 37) add("android.permission.ACCESS_LOCAL_NETWORK")
}
