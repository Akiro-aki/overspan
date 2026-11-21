package app.overspan.viewer;

/** One computer the viewer knows about: found on the network or saved. */
final class Computer {
    final String host;
    final int port;
    String name;
    boolean enabled = true;
    boolean found;
    boolean saved;
    String version = "";
    long seenAt;

    Computer(String host, int port, String name) {
        this.host = host;
        this.port = port;
        this.name = name;
    }

    String key() { return host + ":" + port; }

    String title() { return name == null || name.isEmpty() ? host : name; }

    /** Parses "host", "host:port" or a pasted URL. Returns null if invalid. */
    static Computer parse(String input) {
        String s = input.trim().replaceFirst("^[a-zA-Z]+://", "");
        int slash = s.indexOf('/');
        if (slash >= 0) s = s.substring(0, slash);
        if (s.isEmpty()) return null;
        String host = s;
        int port = 7940;
        int colon = s.lastIndexOf(':');
        if (colon > 0 && s.indexOf(':') == colon) {
            host = s.substring(0, colon);
            try { port = Integer.parseInt(s.substring(colon + 1)); } catch (NumberFormatException e) { return null; }
        }
        if (port < 1 || port > 65535 || !host.matches("[A-Za-z0-9.\\-]+")) return null;
        return new Computer(host, port, "");
    }
}
