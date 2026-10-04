package io.dropcheck.agent;

import android.Manifest;
import android.app.Activity;
import android.content.ComponentName;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.pm.ServiceInfo;
import android.os.Bundle;
import android.os.Process;
import android.widget.TextView;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

/** Separate-UID fixture: only Android/Java APIs are available outside instrumentation. */
public final class UntrustedServiceCallerActivity extends Activity {
    private static final String AGENT_PACKAGE = "io.dropcheck.agent";

    @Override
    @SuppressWarnings("deprecation")
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        File verdict = new File(getFilesDir(), "service-caller-result");
        try {
            writeVerdict(verdict, "FAIL: checks incomplete\n");
            ComponentName component = new ComponentName(AGENT_PACKAGE, AGENT_PACKAGE + ".AgentService");
            ServiceInfo service = getPackageManager().getServiceInfo(component, 0);
            if (Process.myUid() == service.applicationInfo.uid) {
                throw new IllegalStateException("denial test must use a separate UID");
            }
            if (!service.exported || !Manifest.permission.DUMP.equals(service.permission)) {
                throw new IllegalStateException("service must enforce the DUMP component permission");
            }
            if (checkSelfPermission(Manifest.permission.DUMP) != PackageManager.PERMISSION_DENIED) {
                throw new IllegalStateException("test caller must not hold DUMP");
            }
            StringBuilder evidence = new StringBuilder();
            for (String action : new String[] {"GRPC_SESSION", "WIDGET_REFRESH_OBSERVER", "WIDGET_REFRESH_STOP"}) {
                Intent intent = new Intent().setComponent(component).setAction(AGENT_PACKAGE + ".action." + action)
                        .putExtra("grpc_host", "127.0.0.1").putExtra("grpc_port", 43123)
                        .putExtra("grpc_token", "synthetic-denied-session");
                try {
                    startForegroundService(intent);
                    throw new IllegalStateException("cross-UID " + action + " was accepted");
                } catch (SecurityException expected) {
                    if (expected.getMessage() == null || !expected.getMessage().contains(Manifest.permission.DUMP)) {
                        throw new IllegalStateException("denial must be DUMP, not an FGS restriction", expected);
                    }
                    evidence.append("DENIED: ").append(action).append(" exception=")
                            .append(expected.getClass().getName()).append(" permission=")
                            .append(Manifest.permission.DUMP).append('\n');
                }
            }
            String message = "PASS: separate-UID starts denied by DUMP; no shell identity adopted";
            writeVerdict(verdict, message + "\n" + evidence);
            TextView text = new TextView(this);
            text.setText(message);
            setContentView(text);
        } catch (Throwable failure) {
            try {
                writeVerdict(verdict, "FAIL: " + failure.getClass().getSimpleName() + "\n");
            } catch (IOException writeFailure) {
                failure.addSuppressed(writeFailure);
            }
            throw new AssertionError("service caller check failed", failure);
        }
    }

    private static void writeVerdict(File verdict, String message) throws IOException {
        try (FileOutputStream output = new FileOutputStream(verdict)) {
            output.write(message.getBytes(StandardCharsets.UTF_8));
        }
    }
}
