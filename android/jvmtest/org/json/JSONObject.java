package org.json;

import java.util.HashMap;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** JVM stand-in: flat JSON objects only (enough for discovery cards). */
public class JSONObject {
    private final Map<String, String> m = new HashMap<>();
    public JSONObject(String s) throws Exception {
        Matcher x = Pattern.compile("\"(\\w+)\"\\s*:\\s*(\"((?:[^\"\\\\]|\\\\.)*)\"|[-\\w.]+)").matcher(s);
        while (x.find()) m.put(x.group(1), x.group(3) != null ? x.group(3) : x.group(2));
        if (m.isEmpty()) throw new Exception("not json");
    }
    public String optString(String k, String d) { return m.getOrDefault(k, d); }
    public String optString(String k) { return optString(k, ""); }
    public int optInt(String k, int d) { try { return Integer.parseInt(m.get(k)); } catch (Exception e) { return d; } }
    public boolean optBoolean(String k, boolean d) { String v = m.get(k); return v == null ? d : v.equals("true"); }
}
