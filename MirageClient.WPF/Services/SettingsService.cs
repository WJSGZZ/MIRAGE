using System.Globalization;
using System.IO;
using System.Text.Json;

namespace MirageClient.WPF.Services;

public sealed class SettingsService
{
    private readonly string _settingsPath;

    public SettingsService()
    {
        var dir = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
            "MIRAGE",
            "MirageClient.WPF");
        Directory.CreateDirectory(dir);
        _settingsPath = Path.Combine(dir, "settings.json");
    }

    public ClientSettings Load()
    {
        try
        {
            if (!File.Exists(_settingsPath))
            {
                return ClientSettings.CreateDefault();
            }

            var json = File.ReadAllText(_settingsPath);
            var settings = JsonSerializer.Deserialize<ClientSettings>(json);
            return settings ?? ClientSettings.CreateDefault();
        }
        catch
        {
            return ClientSettings.CreateDefault();
        }
    }

    public void Save(ClientSettings settings)
    {
        var json = JsonSerializer.Serialize(settings, new JsonSerializerOptions
        {
            WriteIndented = true
        });
        File.WriteAllText(_settingsPath, json);
    }
}

public sealed class ClientSettings
{
    public string LanguageMode { get; set; } = "system";
    public bool CloseToTray { get; set; } = true;
    public bool MinimizeToTray { get; set; } = true;
    public bool StartMinimizedToTray { get; set; }

    public string ResolveUiLanguage()
    {
        if (LanguageMode == "zh-CN" || LanguageMode == "en-US")
        {
            return LanguageMode;
        }

        var ui = CultureInfo.CurrentUICulture.Name.ToLowerInvariant();
        return ui.StartsWith("zh") ? "zh-CN" : "en-US";
    }

    public static ClientSettings CreateDefault()
    {
        return new ClientSettings();
    }
}
