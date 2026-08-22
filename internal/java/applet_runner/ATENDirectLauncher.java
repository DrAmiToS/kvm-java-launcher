package kvm.launcher;

import java.lang.reflect.Constructor;
import javax.swing.SwingUtilities;

public final class ATENDirectLauncher {
    private ATENDirectLauncher() {
    }

    public static void main(String[] args) throws Exception {
        if (args.length < 2) {
            throw new IllegalArgumentException("usage: ATENDirectLauncher <main-class> <arg>...");
        }

        String mainClass = args[0];
        String[] kvmArgs = new String[args.length - 1];
        System.arraycopy(args, 1, kvmArgs, 0, kvmArgs.length);

        Class<?> clazz = Class.forName(mainClass);
        Constructor<?> constructor = clazz.getConstructor(String[].class);
        Object instance = constructor.newInstance((Object) kvmArgs);
        if (!(instance instanceof Runnable)) {
            throw new IllegalArgumentException(mainClass + " is not Runnable");
        }
        SwingUtilities.invokeLater((Runnable) instance);
    }
}
