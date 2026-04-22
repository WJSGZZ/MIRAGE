using System.Diagnostics;
using System.IO;
using System.Net.Http;

namespace MirageClient.WPF.Services;

public sealed class CoreHostService : IDisposable
{
    private const string CoreHealthUrl = "http://127.0.0.1:9099/health";

    private readonly HttpClient _http = new()
    {
        Timeout = TimeSpan.FromSeconds(2),
    };

    private Process? _process;
    private bool _startedByClient;
    private readonly string _logPath;

    public string DataDirectory { get; }
    public string ServersFile { get; }
    public string? CorePath { get; private set; }

    public CoreHostService()
    {
        DataDirectory = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
            "MIRAGE",
            "MirageClient.WPF");
        Directory.CreateDirectory(DataDirectory);
        ServersFile = Path.Combine(DataDirectory, "servers.json");
        _logPath = Path.Combine(DataDirectory, "core-host.log");
    }

    public async Task EnsureRunningAsync(CancellationToken ct = default)
    {
        Log("EnsureRunningAsync invoked");
        if (await IsHealthyAsync(ct))
        {
            Log("Core already healthy");
            return;
        }

        var corePath = FindCoreExecutable();
        if (string.IsNullOrWhiteSpace(corePath))
        {
            Log("Core executable not found");
            return;
        }

        CorePath = corePath;
        Log($"Starting core from {corePath}");
        var process = new Process
        {
            StartInfo = new ProcessStartInfo
            {
                FileName = corePath,
                Arguments = $"--servers \"{ServersFile}\" --no-browser",
                WorkingDirectory = Path.GetDirectoryName(corePath) ?? AppContext.BaseDirectory,
                UseShellExecute = false,
                CreateNoWindow = true,
            },
            EnableRaisingEvents = true,
        };

        process.Start();
        _process = process;
        _startedByClient = true;
        Log($"Core process started with PID {process.Id}");

        var deadline = DateTime.UtcNow.AddSeconds(10);
        while (DateTime.UtcNow < deadline && !ct.IsCancellationRequested)
        {
            if (await IsHealthyAsync(ct))
            {
                Log("Core reported healthy");
                return;
            }

            if (_process.HasExited)
            {
                Log($"Core exited early with code {_process.ExitCode}");
                return;
            }

            await Task.Delay(250, ct);
        }
        Log("Timed out waiting for core health");
    }

    public async Task ShutdownAsync()
    {
        try
        {
            if (_startedByClient)
            {
                try
                {
                    using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(2));
                    await _http.PostAsync("http://127.0.0.1:9099/disconnect", null, cts.Token);
                }
                catch
                {
                    // Best effort cleanup only.
                }
            }
        }
        finally
        {
            if (_process is { HasExited: false })
            {
                try
                {
                    _process.Kill(entireProcessTree: true);
                    _process.WaitForExit(3000);
                }
                catch
                {
                    // Ignore shutdown race.
                }
            }
        }
    }

    public void Dispose()
    {
        _http.Dispose();
        _process?.Dispose();
    }

    private async Task<bool> IsHealthyAsync(CancellationToken ct)
    {
        try
        {
            using var resp = await _http.GetAsync(CoreHealthUrl, ct);
            return resp.IsSuccessStatusCode;
        }
        catch
        {
            return false;
        }
    }

    private static string? FindCoreExecutable()
    {
        var candidates = new[]
        {
            Path.Combine(AppContext.BaseDirectory, "miragec.exe"),
            Path.Combine(AppContext.BaseDirectory, "core", "miragec.exe"),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "miragec.exe")),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "miragec.exe")),
        };

        return candidates.FirstOrDefault(File.Exists);
    }

    private void Log(string message)
    {
        try
        {
            File.AppendAllText(_logPath, $"[{DateTime.Now:yyyy-MM-dd HH:mm:ss}] {message}{Environment.NewLine}");
        }
        catch
        {
            // Ignore logging failures.
        }
    }
}
