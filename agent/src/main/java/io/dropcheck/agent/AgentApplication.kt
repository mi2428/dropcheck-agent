package io.dropcheck.agent

import android.app.Application
import java.io.File

class AgentApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        OuiDatabase.current = OuiDatabase(File(filesDir, "oui/registry.tsv.gz"))
    }
}
