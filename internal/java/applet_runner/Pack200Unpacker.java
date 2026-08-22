package kvm.launcher;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.file.Files;
import java.util.jar.JarOutputStream;
import java.util.jar.Pack200;
import java.util.zip.GZIPInputStream;

public final class Pack200Unpacker {
    private Pack200Unpacker() {
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("usage: Pack200Unpacker <input.pack.gz> <output.jar>");
        }

        File input = new File(args[0]);
        File output = new File(args[1]);
        File parent = output.getParentFile();
        if (parent != null) {
            parent.mkdirs();
        }

        try (InputStream file = Files.newInputStream(input.toPath());
             InputStream in = gzipMaybe(file);
             OutputStream out = new FileOutputStream(output);
             JarOutputStream jar = new JarOutputStream(out)) {
            Pack200.newUnpacker().unpack(in, jar);
        }
    }

    private static InputStream gzipMaybe(InputStream in) throws Exception {
        if (!in.markSupported()) {
            in = new java.io.BufferedInputStream(in);
        }
        in.mark(2);
        int b1 = in.read();
        int b2 = in.read();
        in.reset();
        if (b1 == 0x1f && b2 == 0x8b) {
            return new GZIPInputStream(in);
        }
        return in;
    }
}
