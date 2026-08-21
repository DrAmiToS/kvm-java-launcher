package kvm.launcher;

import java.applet.Applet;
import java.applet.AppletContext;
import java.applet.AppletStub;
import java.applet.AudioClip;
import java.awt.BorderLayout;
import java.awt.Frame;
import java.awt.Image;
import java.awt.Panel;
import java.awt.Toolkit;
import java.awt.Window;
import java.awt.event.ComponentAdapter;
import java.awt.event.ComponentEvent;
import java.awt.event.WindowAdapter;
import java.awt.event.WindowEvent;
import java.io.InputStream;
import java.io.OutputStream;
import java.io.PrintStream;
import java.net.URL;
import java.util.Collections;
import java.util.Enumeration;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Iterator;
import java.util.Map;
import java.util.Set;
import java.security.SecureRandom;
import java.security.cert.X509Certificate;
import javax.net.ssl.HostnameVerifier;
import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSession;
import javax.net.ssl.TrustManager;
import javax.net.ssl.X509TrustManager;

public final class AppletRunner {
    private AppletRunner() {
    }

    public static void main(String[] args) throws Exception {
        if (args.length < 2) {
            throw new IllegalArgumentException("usage: AppletRunner <main-class> <document-base> [name=value...]");
        }

        trustAllCertificates();
        installExitOnAppletStopOutput();

        String mainClass = args[0];
        URL documentBase = new URL(args[1]);
        URL codeBase = new URL(documentBase, "./");
        Map params = new HashMap();
        for (int i = 2; i < args.length; i++) {
            int eq = args[i].indexOf('=');
            if (eq > 0) {
                params.put(args[i].substring(0, eq), args[i].substring(eq + 1));
            }
        }

        Class clazz = Class.forName(mainClass);
        final Applet applet = (Applet) clazz.getDeclaredConstructor().newInstance();
        applet.setStub(new Stub(documentBase, codeBase, params));

        final Frame frame = new Frame("KVM Java Applet");
        frame.setLayout(new BorderLayout());
        Panel panel = new Panel(new BorderLayout());
        panel.add(applet, BorderLayout.CENTER);
        frame.add(panel, BorderLayout.CENTER);
        applet.setSize(800, 600);
        frame.setUndecorated(true);
        frame.setSize(800, 600);
        frame.setLocation(-32000, -32000);
        frame.addWindowListener(new WindowAdapter() {
            public void windowClosing(WindowEvent event) {
                shutdown(applet, frame);
            }
        });

        frame.setVisible(true);
        frame.validate();
        applet.init();
        applet.start();
        startWindowWatchdog(applet, frame);
    }

    private static void trustAllCertificates() throws Exception {
        TrustManager[] trustManagers = new TrustManager[] {
            new X509TrustManager() {
                public X509Certificate[] getAcceptedIssuers() {
                    return new X509Certificate[0];
                }

                public void checkClientTrusted(X509Certificate[] certs, String authType) {
                }

                public void checkServerTrusted(X509Certificate[] certs, String authType) {
                }
            }
        };
        SSLContext context = SSLContext.getInstance("TLS");
        context.init(null, trustManagers, new SecureRandom());
        SSLContext.setDefault(context);
        HttpsURLConnection.setDefaultSSLSocketFactory(context.getSocketFactory());
        HttpsURLConnection.setDefaultHostnameVerifier(new HostnameVerifier() {
            public boolean verify(String hostname, SSLSession session) {
                return true;
            }
        });
    }

    private static void startWindowWatchdog(final Applet applet, final Frame container) {
        Thread thread = new Thread(new Runnable() {
            public void run() {
                boolean sawExternalWindow = false;
                Set watched = Collections.newSetFromMap(new IdentityHashMap());
                while (true) {
                    try {
                        Thread.sleep(500);
                    } catch (InterruptedException ignored) {
                    }
                    int visibleExternalWindows = 0;
                    Window[] windows = Window.getWindows();
                    for (int i = 0; i < windows.length; i++) {
                        Window window = windows[i];
                        if (window == container) {
                            continue;
                        }
                        if (!watched.contains(window)) {
                            watched.add(window);
                            watchExternalWindow(window, applet, container);
                        }
                        if (window.isShowing()) {
                            visibleExternalWindows++;
                        }
                    }
                    if (visibleExternalWindows > 0) {
                        sawExternalWindow = true;
                    } else if (sawExternalWindow) {
                        shutdown(applet, container);
                    }
                }
            }
        }, "applet-window-watchdog");
        thread.setDaemon(true);
        thread.start();
    }

    private static void watchExternalWindow(Window window, final Applet applet, final Frame container) {
        final boolean[] shutdownStarted = new boolean[] {false};
        final Runnable delayedShutdown = new Runnable() {
            public void run() {
                synchronized (shutdownStarted) {
                    if (shutdownStarted[0]) {
                        return;
                    }
                    shutdownStarted[0] = true;
                }
                Thread thread = new Thread(new Runnable() {
                    public void run() {
                        try {
                            Thread.sleep(1500);
                        } catch (InterruptedException ignored) {
                        }
                        shutdown(applet, container);
                    }
                }, "applet-window-close-shutdown");
                thread.setDaemon(false);
                thread.start();
            }
        };
        window.addWindowListener(new WindowAdapter() {
            public void windowClosing(WindowEvent event) {
                delayedShutdown.run();
            }

            public void windowClosed(WindowEvent event) {
                delayedShutdown.run();
            }
        });
        window.addComponentListener(new ComponentAdapter() {
            public void componentHidden(ComponentEvent event) {
                delayedShutdown.run();
            }
        });
    }

    private static void shutdown(Applet applet, Frame container) {
        try {
            applet.stop();
            applet.destroy();
        } finally {
            container.dispose();
            System.exit(0);
        }
    }

    private static void installExitOnAppletStopOutput() {
        final PrintStream originalOut = System.out;
        System.setOut(new PrintStream(new OutputStream() {
            private final StringBuffer line = new StringBuffer();

            public synchronized void write(int value) {
                originalOut.write(value);
                if (value == '\n' || value == '\r') {
                    checkLine(line.toString());
                    line.setLength(0);
                    return;
                }
                line.append((char) value);
            }

            private void checkLine(String text) {
                if (text.indexOf("Intgapp stopped running") >= 0 || text.indexOf("Hiding applet") >= 0) {
                    exitSoon();
                }
            }
        }, true));
    }

    private static void exitSoon() {
        Thread thread = new Thread(new Runnable() {
            public void run() {
                try {
                    Thread.sleep(1500);
                } catch (InterruptedException ignored) {
                }
                System.exit(0);
            }
        }, "applet-output-shutdown");
        thread.setDaemon(false);
        thread.start();
    }

    private static final class Stub implements AppletStub, AppletContext {
        private final URL documentBase;
        private final URL codeBase;
        private final Map params;

        Stub(URL documentBase, URL codeBase, Map params) {
            this.documentBase = documentBase;
            this.codeBase = codeBase;
            this.params = params;
        }

        public boolean isActive() {
            return true;
        }

        public URL getDocumentBase() {
            return documentBase;
        }

        public URL getCodeBase() {
            return codeBase;
        }

        public String getParameter(String name) {
            Object value = params.get(name);
            return value == null ? null : value.toString();
        }

        public AppletContext getAppletContext() {
            return this;
        }

        public void appletResize(int width, int height) {
        }

        public AudioClip getAudioClip(URL url) {
            return Applet.newAudioClip(url);
        }

        public Image getImage(URL url) {
            return Toolkit.getDefaultToolkit().createImage(url);
        }

        public Applet getApplet(String name) {
            return null;
        }

        public Enumeration getApplets() {
            return Collections.enumeration(Collections.emptyList());
        }

        public void showDocument(URL url) {
        }

        public void showDocument(URL url, String target) {
        }

        public void showStatus(String status) {
            System.out.println(status);
        }

        public void setStream(String key, InputStream stream) {
        }

        public InputStream getStream(String key) {
            return null;
        }

        public Iterator getStreamKeys() {
            return Collections.emptyList().iterator();
        }
    }
}
