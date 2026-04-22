using System.Net.Http;
using System.Net.Http.Json;
using MirageClient.WPF.Models;

namespace MirageClient.WPF.Services;

public sealed class CoreApiClient
{
    private readonly HttpClient _http;

    public CoreApiClient()
    {
        _http = new HttpClient
        {
            BaseAddress = new Uri("http://127.0.0.1:9099")
        };
    }

    public async Task<HealthInfo?> GetHealthAsync(CancellationToken ct)
        => await _http.GetFromJsonAsync<HealthInfo>("/health", ct);

    public async Task<VersionInfo?> GetVersionAsync(CancellationToken ct)
        => await _http.GetFromJsonAsync<VersionInfo>("/version", ct);

    public async Task<CoreState?> GetStateAsync(CancellationToken ct)
        => await _http.GetFromJsonAsync<CoreState>("/state", ct);

    public async Task<CoreStats?> GetStatsAsync(CancellationToken ct)
        => await _http.GetFromJsonAsync<CoreStats>("/stats", ct);

    public async Task<List<ProfileItem>> GetProfilesAsync(CancellationToken ct)
        => await _http.GetFromJsonAsync<List<ProfileItem>>("/profiles", ct) ?? [];

    public async Task<ProxyConfigInfo?> GetProxyConfigAsync(CancellationToken ct)
        => await _http.GetFromJsonAsync<ProxyConfigInfo>("/proxy/config", ct);

    public async Task<LogEnvelope?> GetLogsAsync(string? since, CancellationToken ct)
    {
        var path = string.IsNullOrWhiteSpace(since)
            ? "/logs"
            : $"/logs?since={Uri.EscapeDataString(since)}";
        return await _http.GetFromJsonAsync<LogEnvelope>(path, ct);
    }

    public async Task ConnectAsync(string profileId, CancellationToken ct)
    {
        using var resp = await _http.PostAsJsonAsync("/connect", new { profile = profileId }, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task DisconnectAsync(CancellationToken ct)
    {
        using var resp = await _http.PostAsync("/disconnect", null, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task ImportAsync(string uri, CancellationToken ct)
    {
        using var resp = await _http.PostAsJsonAsync("/api/import", new { uri }, ct);
        resp.EnsureSuccessStatusCode();
    }

    public async Task<ProxyConfigInfo?> UpdateProxyConfigAsync(ProxyConfigUpdate update, CancellationToken ct)
    {
        using var resp = await _http.PostAsJsonAsync("/proxy/config", update, ct);
        resp.EnsureSuccessStatusCode();
        return await resp.Content.ReadFromJsonAsync<ProxyConfigInfo>(cancellationToken: ct);
    }

    public async Task<ProxyConfigInfo?> ReapplyProxyAsync(CancellationToken ct)
    {
        using var resp = await _http.PostAsync("/proxy/reapply", null, ct);
        resp.EnsureSuccessStatusCode();
        return await resp.Content.ReadFromJsonAsync<ProxyConfigInfo>(cancellationToken: ct);
    }

    public async Task<string> GetDiagnosticsTextAsync(string? target, CancellationToken ct)
    {
        var path = string.IsNullOrWhiteSpace(target)
            ? "/api/diagnostics/text"
            : $"/api/diagnostics/text?target={Uri.EscapeDataString(target)}";
        return await _http.GetStringAsync(path, ct);
    }
}
