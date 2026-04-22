using System.Text.Json.Serialization;

namespace MirageClient.WPF.Models;

public sealed class CoreState
{
    [JsonPropertyName("running")]
    public bool Running { get; set; }

    [JsonPropertyName("status")]
    public string Status { get; set; } = "idle";

    [JsonPropertyName("socks5")]
    public string Socks5 { get; set; } = "";

    [JsonPropertyName("http")]
    public string Http { get; set; } = "";

    [JsonPropertyName("envHttp")]
    public string EnvHttp { get; set; } = "";

    [JsonPropertyName("envAll")]
    public string EnvAll { get; set; } = "";

    [JsonPropertyName("proxyScope")]
    public string ProxyScope { get; set; } = "";

    [JsonPropertyName("captureGap")]
    public string CaptureGap { get; set; } = "";

    [JsonPropertyName("activeId")]
    public string ActiveId { get; set; } = "";

    [JsonPropertyName("proxyMode")]
    public string ProxyMode { get; set; } = "system";

    [JsonPropertyName("proxyApplied")]
    public bool ProxyApplied { get; set; }

    [JsonPropertyName("applyWinHttp")]
    public bool ApplyWinHttp { get; set; } = true;

    [JsonPropertyName("exportEnv")]
    public bool ExportEnv { get; set; } = true;

    [JsonPropertyName("pacUrl")]
    public string PacUrl { get; set; } = "";

    [JsonPropertyName("lastProxyApplyAt")]
    public string LastProxyApplyAt { get; set; } = "";

    [JsonPropertyName("lastProxyApplyError")]
    public string LastProxyApplyError { get; set; } = "";
}

public sealed class CoreStats
{
    [JsonPropertyName("running")]
    public bool Running { get; set; }

    [JsonPropertyName("activeProfile")]
    public string ActiveProfile { get; set; } = "";

    [JsonPropertyName("uptimeSeconds")]
    public long UptimeSeconds { get; set; }

    [JsonPropertyName("uploadBytes")]
    public long UploadBytes { get; set; }

    [JsonPropertyName("downloadBytes")]
    public long DownloadBytes { get; set; }

    [JsonPropertyName("uploadRateBps")]
    public long UploadRateBps { get; set; }

    [JsonPropertyName("downloadRateBps")]
    public long DownloadRateBps { get; set; }
}

public sealed class ProfileItem
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("name")]
    public string Name { get; set; } = "";

    [JsonPropertyName("server")]
    public string Server { get; set; } = "";

    [JsonPropertyName("sni")]
    public string Sni { get; set; } = "";

    [JsonPropertyName("proxyMode")]
    public string ProxyMode { get; set; } = "system";

    [JsonPropertyName("active")]
    public bool Active { get; set; }
}

public sealed class LogEnvelope
{
    [JsonPropertyName("items")]
    public List<LogItem> Items { get; set; } = [];
}

public sealed class LogItem
{
    [JsonPropertyName("timestamp")]
    public string Timestamp { get; set; } = "";

    [JsonPropertyName("message")]
    public string Message { get; set; } = "";
}

public sealed class VersionInfo
{
    [JsonPropertyName("core")]
    public string Core { get; set; } = "";

    [JsonPropertyName("protocol")]
    public string Protocol { get; set; } = "";
}

public sealed class HealthInfo
{
    [JsonPropertyName("ok")]
    public bool Ok { get; set; }

    [JsonPropertyName("ready")]
    public bool Ready { get; set; }

    [JsonPropertyName("running")]
    public bool Running { get; set; }
}

public sealed class ProxySystemSnapshot
{
    [JsonPropertyName("proxyEnable")]
    public string ProxyEnable { get; set; } = "";

    [JsonPropertyName("proxyServer")]
    public string ProxyServer { get; set; } = "";

    [JsonPropertyName("proxyOverride")]
    public string ProxyOverride { get; set; } = "";

    [JsonPropertyName("autoConfigUrl")]
    public string AutoConfigUrl { get; set; } = "";

    [JsonPropertyName("autoDetect")]
    public string AutoDetect { get; set; } = "";

    [JsonPropertyName("winHttp")]
    public string WinHttp { get; set; } = "";

    [JsonPropertyName("env")]
    public Dictionary<string, string> Env { get; set; } = [];
}

public sealed class ProxyConfigInfo
{
    [JsonPropertyName("mode")]
    public string Mode { get; set; } = "system";

    [JsonPropertyName("applyWinHttp")]
    public bool ApplyWinHttp { get; set; } = true;

    [JsonPropertyName("exportEnv")]
    public bool ExportEnv { get; set; } = true;

    [JsonPropertyName("pacUrl")]
    public string PacUrl { get; set; } = "";

    [JsonPropertyName("pacRunning")]
    public bool PacRunning { get; set; }

    [JsonPropertyName("applied")]
    public bool Applied { get; set; }

    [JsonPropertyName("lastApplyAt")]
    public string LastApplyAt { get; set; } = "";

    [JsonPropertyName("lastApplyError")]
    public string LastApplyError { get; set; } = "";

    [JsonPropertyName("system")]
    public ProxySystemSnapshot System { get; set; } = new();
}

public sealed class ProxyConfigUpdate
{
    [JsonPropertyName("mode")]
    public string Mode { get; set; } = "system";

    [JsonPropertyName("applyWinHttp")]
    public bool ApplyWinHttp { get; set; } = true;

    [JsonPropertyName("exportEnv")]
    public bool ExportEnv { get; set; } = true;
}
