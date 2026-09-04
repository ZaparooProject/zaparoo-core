// Zaparoo LaunchBox Plugin
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

using System.IO;
using System.IO.Pipes;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Windows.Input;
using System.Windows.Threading;
using System.Xml.Linq;
using System.Xml.XPath;
using Unbroken.LaunchBox.Plugins;
using Unbroken.LaunchBox.Plugins.Data;

namespace ZaparooLaunchBoxPlugin;

/// <summary>
/// Zaparoo plugin for LaunchBox that enables bidirectional communication
/// via named pipes for game launching and lifecycle event tracking.
/// </summary>
public class ZaparooPlugin : ISystemEventsPlugin, IGameLaunchingPlugin, IGameMenuItemPlugin
{
    private const string PipeName = "zaparoo-launchbox-ipc";
    private const uint WinEventOutOfContext = 0;
    private const uint EventSystemForeground = 3;

    // ProtocolVersion is announced to Core in the Hello event. Bump it when
    // adding commands so Core can tell what this plugin understands.
    // 2 added the Stop command and the Pid field on MediaStarted.
    private const int ProtocolVersion = 2;

    // Read from the assembly so the csproj stays the single source of truth.
    private static string PluginVersion =>
        Assembly.GetExecutingAssembly().GetName().Version?.ToString(3) ?? "0.0.0";

    // How long to keep looking for the process LaunchBox started. Emulators
    // behind a launcher stub can take a moment to appear.
    private const int ProcessResolveAttempts = 20;
    private const int ProcessResolveDelayMs = 100;
    private const int StopWaitMs = 10000;

    // Static connection state - shared across all plugin instances
    private static NamedPipeClientStream? _pipeClient;
    private static StreamWriter? _pipeWriter;
    private static StreamReader? _pipeReader;
    private static CancellationTokenSource? _cancellationTokenSource;
    private static Task? _connectionTask;
    private static readonly object _pipeLock = new();
    private static readonly object _stateLock = new();
    private static bool _isShuttingDown;
    private static bool _isBigBoxRunning;
    private static Dispatcher? _uiDispatcher;
    private static string _launchBoxRoot = string.Empty;
    private static IGame? _currentGame;
    private static IAdditionalApplication? _currentAdditionalApp;
    private static int _currentGamePid;
    private static int _launchGeneration;
    private static string? _pendingLaunchGameId;
    private static string? _pendingLaunchAdditionalAppId;
    private static bool _pendingLaunchShouldNavigate;
    private static bool _shouldNavigateBackAfterLaunch;
    private static bool _shouldShowGameAfterFocusLoss;
    private static WinEventDelegate? _foregroundDelegate;
    private static nint _foregroundHook;

    public string Name => "Zaparoo LaunchBox Integration";

    private delegate void WinEventDelegate(
        nint hWinEventHook,
        uint eventType,
        nint hwnd,
        int idObject,
        int idChild,
        uint dwEventThread,
        uint dwmsEventTime
    );

    [DllImport("user32.dll")]
    private static extern nint SetWinEventHook(
        uint eventMin,
        uint eventMax,
        nint hmodWinEventProc,
        WinEventDelegate lpfnWinEventProc,
        uint idProcess,
        uint idThread,
        uint dwFlags
    );

    [DllImport("user32.dll")]
    private static extern bool UnhookWinEvent(nint hWinEventHook);

    [DllImport("user32.dll")]
    private static extern nint GetForegroundWindow();

    [DllImport("user32.dll")]
    private static extern int GetWindowText(nint hWnd, StringBuilder text, int count);

    [DllImport("user32.dll", SetLastError = true)]
    private static extern void keybd_event(byte bVk, byte bScan, int dwFlags, int dwExtraInfo);

    // Constructor - try to connect immediately as fallback
    public ZaparooPlugin()
    {
        CaptureApplicationContext();

        // Try to connect immediately in case system events don't fire
        System.Threading.Tasks.Task.Run(() =>
        {
            System.Threading.Thread.Sleep(2000); // Wait 2 seconds for LaunchBox to fully initialize
            if (_connectionTask == null || _connectionTask.IsCompleted)
            {
                StartConnectionTask();
            }
        });
    }

    #region ISystemEventsPlugin Implementation

    public void OnEventRaised(string eventType)
    {
        CaptureApplicationContext();

        if (eventType == SystemEventTypes.PluginInitialized)
        {
            _uiDispatcher ??= System.Windows.Application.Current?.Dispatcher;
        }
        else if (eventType == SystemEventTypes.LaunchBoxStartupCompleted)
        {
            _uiDispatcher ??= System.Windows.Application.Current?.Dispatcher;
            _isBigBoxRunning = false;
            _isShuttingDown = false;
            StartConnectionTask();
        }
        else if (eventType == SystemEventTypes.BigBoxStartupCompleted)
        {
            _uiDispatcher ??= System.Windows.Application.Current?.Dispatcher;
            _isBigBoxRunning = true;
            _isShuttingDown = false;
            EnsureForegroundHook();
            StartConnectionTask();
        }
        else if (eventType == SystemEventTypes.SelectionChanged)
        {
            ClearCurrentGameIfSelectionChanged();
        }
        else if (eventType == SystemEventTypes.GameExited)
        {
            NavigateBackAfterZaparooLaunch();
        }
        else if (eventType == SystemEventTypes.LaunchBoxShutdownBeginning ||
                 eventType == SystemEventTypes.BigBoxShutdownBeginning)
        {
            _isShuttingDown = true;
            ClearPendingLaunchState();
            StopForegroundHook();
            DisconnectFromPipe();
        }
    }

    #endregion

    #region IGameLaunchingPlugin Implementation

    public void OnBeforeGameLaunching(IGame game, IAdditionalApplication? app, IEmulator? emulator)
    {
        lock (_stateLock)
        {
            _currentGame = game;
            _currentAdditionalApp = app;
            _shouldNavigateBackAfterLaunch = MatchesPendingLaunch(game, app) && _pendingLaunchShouldNavigate;
            _shouldShowGameAfterFocusLoss = _shouldNavigateBackAfterLaunch;
            _pendingLaunchGameId = null;
            _pendingLaunchAdditionalAppId = null;
            _pendingLaunchShouldNavigate = false;
        }

        Log($"Game launching: {game.Title} ({game.Id})");
    }

    public void OnAfterGameLaunched(IGame game, IAdditionalApplication? app, IEmulator? emulator)
    {
        // Read everything off the LaunchBox objects on this thread; the
        // resolution below runs on a worker and must not touch them.
        string id = app?.Id ?? game.Id;
        string? target = emulator?.ApplicationPath;
        if (string.IsNullOrWhiteSpace(target))
        {
            target = app?.ApplicationPath ?? game.ApplicationPath;
        }

        int generation;
        lock (_stateLock)
        {
            generation = ++_launchGeneration;
            _currentGamePid = 0;
        }

        // Announce the launch straight away. Core publishes active media from
        // this event, so gating it on process discovery would leave the user
        // staring at nothing for as long as the search takes.
        SendEvent(new GameStartedEvent
        {
            Event = "MediaStarted",
            Id = id,
            Title = app?.Name ?? game.Title,
            Platform = game.Platform,
            ApplicationPath = app?.ApplicationPath ?? game.ApplicationPath
        });

        // The process can take a couple of seconds to appear, so it is
        // resolved on a worker and reported separately. The generation check
        // discards a slow search whose game has already been replaced, which
        // would otherwise report game A's PID as if it were game B's.
        System.Threading.Tasks.Task.Run(() =>
        {
            int pid = ResolveGameProcessId(target);
            if (pid == 0)
            {
                return;
            }

            lock (_stateLock)
            {
                if (_launchGeneration != generation)
                {
                    Log($"Discarding stale process {pid} for replaced launch {id}");
                    return;
                }
                _currentGamePid = pid;
            }

            SendEvent(new GameProcessEvent { Id = id, Pid = pid });
        });
    }

    /// <summary>
    /// Finds the process LaunchBox started for a game, so Core can stop the
    /// whole tree if asked. Returns 0 when it cannot be determined, which is
    /// normal for entries that launch a URL such as steam://.
    /// </summary>
    private static int ResolveGameProcessId(string? target)
    {
        if (string.IsNullOrWhiteSpace(target) || target.Contains("://", StringComparison.Ordinal))
        {
            return 0;
        }

        string name;
        string full;
        try
        {
            name = Path.GetFileNameWithoutExtension(target);
            full = Path.GetFullPath(target);
        }
        catch (Exception ex)
        {
            Log($"Could not parse launch target '{target}': {ex.Message}");
            return 0;
        }

        if (string.IsNullOrEmpty(name))
        {
            return 0;
        }

        for (int attempt = 0; attempt < ProcessResolveAttempts; attempt++)
        {
            int pid = FindNewestProcess(name, full);
            if (pid != 0)
            {
                Log($"Resolved game process {pid} for {name}");
                return pid;
            }
            System.Threading.Thread.Sleep(ProcessResolveDelayMs);
        }

        Log($"Could not resolve a process for {name}");
        return 0;
    }

    private static int FindNewestProcess(string name, string fullPath)
    {
        System.Diagnostics.Process? best = null;
        var candidates = System.Diagnostics.Process.GetProcessesByName(name);

        foreach (var proc in candidates)
        {
            try
            {
                if (proc.HasExited)
                {
                    continue;
                }

                // MainModule throws for processes at a higher integrity level;
                // treat those as a name-only match rather than discarding them.
                string? path = null;
                try
                {
                    path = proc.MainModule?.FileName;
                }
                catch (Exception)
                {
                    path = null;
                }

                if (path != null && !string.Equals(path, fullPath, StringComparison.OrdinalIgnoreCase))
                {
                    continue;
                }

                if (best == null || proc.StartTime > best.StartTime)
                {
                    best = proc;
                }
            }
            catch (Exception)
            {
                // Process exited between enumeration and inspection.
            }
        }

        int pid = best?.Id ?? 0;

        // Every enumerated process holds a handle until disposed, and this
        // runs up to ProcessResolveAttempts times per launch.
        foreach (var proc in candidates)
        {
            proc.Dispose();
        }

        return pid;
    }

    /// <summary>
    /// Ends the running game and reports the outcome back to Core. LaunchBox
    /// exposes no stop API to plugins, so the tracked process tree is ended
    /// directly; LaunchBox notices and raises its own GameExited afterwards.
    /// </summary>
    private void StopRunningGame(string? requestedId)
    {
        IGame? game;
        IAdditionalApplication? app;
        int pid;
        lock (_stateLock)
        {
            game = _currentGame;
            app = _currentAdditionalApp;
            pid = _currentGamePid;
        }

        string runningId = app?.Id ?? game?.Id ?? string.Empty;
        string replyId = string.IsNullOrEmpty(requestedId) ? runningId : requestedId!;

        if (game == null)
        {
            SendStopResult(replyId, "failed", "no game is running");
            return;
        }

        if (!string.IsNullOrEmpty(requestedId) &&
            !string.Equals(requestedId, runningId, StringComparison.OrdinalIgnoreCase))
        {
            SendStopResult(replyId, "failed", $"game {requestedId} is not the running game");
            return;
        }

        if (pid <= 0)
        {
            SendStopResult(replyId, "failed", "no tracked process for the running game");
            return;
        }

        try
        {
            var proc = System.Diagnostics.Process.GetProcessById(pid);
            proc.Kill(entireProcessTree: true);
            proc.WaitForExit(StopWaitMs);
            if (proc.HasExited)
            {
                lock (_stateLock)
                {
                    _currentGamePid = 0;
                }
                SendStopResult(replyId, "completed", string.Empty);
            }
            else
            {
                SendStopResult(replyId, "failed", "process did not exit");
            }
        }
        catch (Exception ex) when (ex is ArgumentException or InvalidOperationException)
        {
            // The process exited on its own between lookup and kill, which is
            // the outcome the caller wanted.
            lock (_stateLock)
            {
                _currentGamePid = 0;
            }
            SendStopResult(replyId, "completed", string.Empty);
        }
        catch (Exception ex)
        {
            SendStopResult(replyId, "failed", ex.Message);
        }
    }

    private void SendStopResult(string id, string status, string error)
    {
        Log($"Stop result for {id}: {status} {error}");
        SendEvent(new StopResultEvent
        {
            Id = id,
            Status = status,
            Error = error
        });
    }

    public void OnGameExited()
    {
        IGame? game;
        IAdditionalApplication? app;
        lock (_stateLock)
        {
            game = _currentGame;
            app = _currentAdditionalApp;
            _currentGame = null;
            _currentAdditionalApp = null;
            _currentGamePid = 0;
            _shouldShowGameAfterFocusLoss = false;
        }

        if (game != null)
        {
            SendEvent(new GameExitedEvent
            {
                Event = "MediaStopped",
                Id = app?.Id ?? game.Id,
                Title = app?.Name ?? game.Title
            });
        }

        NavigateBackAfterZaparooLaunch();
    }

    #endregion

    #region IGameMenuItemPlugin Implementation

    public string Caption => "Write to Tag";

    public System.Drawing.Image? IconImage => null;

    public bool ShowInLaunchBox => true;

    public bool ShowInBigBox => true;

    public bool SupportsMultipleGames => false;

    public bool GetIsValidForGame(IGame game)
    {
        // Show menu item for all games
        return true;
    }

    public bool GetIsValidForGames(IGame[] games)
    {
        // Not used since SupportsMultipleGames is false
        return false;
    }

    public void OnSelected(IGame game)
    {
        // User selected "Write to Tag" - send request to Zaparoo
        SendEvent(new WriteRequestEvent
        {
            Event = "Write",
            Id = game.Id,
            Title = game.Title,
            Platform = game.Platform
        });

        // Show feedback to user
        System.Windows.MessageBox.Show(
            $"Scan an NFC tag on the reader to write:\n\n{game.Title}",
            "Write to Tag - Zaparoo",
            System.Windows.MessageBoxButton.OK,
            System.Windows.MessageBoxImage.Information
        );
    }

    public void OnSelected(IGame[] games)
    {
        // Not used since SupportsMultipleGames is false
    }

    #endregion

    #region LaunchBox Context and Logging

    private static void CaptureApplicationContext()
    {
        _uiDispatcher ??= System.Windows.Application.Current?.Dispatcher;

        if (!string.IsNullOrEmpty(_launchBoxRoot))
        {
            return;
        }

        try
        {
            string? root = Path.GetDirectoryName(System.Diagnostics.Process.GetCurrentProcess().MainModule?.FileName);
            if (string.IsNullOrEmpty(root))
            {
                root = Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location);
            }

            if (!string.IsNullOrEmpty(root) &&
                (root.EndsWith($"{Path.DirectorySeparatorChar}core", StringComparison.OrdinalIgnoreCase) ||
                 root.EndsWith($"{Path.AltDirectorySeparatorChar}core", StringComparison.OrdinalIgnoreCase)))
            {
                root = Directory.GetParent(root)?.FullName;
            }

            _launchBoxRoot = root ?? string.Empty;
        }
        catch (Exception ex)
        {
            System.Diagnostics.Debug.WriteLine($"Zaparoo plugin: Failed to capture LaunchBox root: {ex.Message}");
        }
    }

    private static readonly object _logLock = new();

    private static void Log(string message)
    {
        try
        {
            CaptureApplicationContext();
            string root = _launchBoxRoot;
            if (string.IsNullOrEmpty(root))
            {
                root = Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location) ?? string.Empty;
            }

            if (string.IsNullOrEmpty(root))
            {
                System.Diagnostics.Debug.WriteLine($"Zaparoo plugin: {message}");
                return;
            }

            string logDir = Path.Combine(root, "Logs");
            Directory.CreateDirectory(logDir);
            string logPath = Path.Combine(logDir, "ZaparooLaunchBoxPlugin.txt");
            // Called from the pipe loop, the resolver worker and the UI
            // thread; concurrent appends would throw and silently drop lines.
            lock (_logLock)
            {
                File.AppendAllText(logPath, $"[{DateTime.Now}] {message}{Environment.NewLine}");
            }
        }
        catch (Exception ex)
        {
            System.Diagnostics.Debug.WriteLine($"Zaparoo plugin: {message} (log failed: {ex.Message})");
        }
    }

    #endregion

    #region Named Pipe Communication

    private void StartConnectionTask()
    {
        lock (_pipeLock)
        {
            // Don't start a new task if one is already running or if we are shutting down
            if (_isShuttingDown || (_connectionTask != null && !_connectionTask.IsCompleted))
            {
                return;
            }

            // Single CancellationTokenSource manages the entire connection lifecycle
            _cancellationTokenSource?.Dispose();
            _cancellationTokenSource = new CancellationTokenSource();

            _connectionTask = Task.Run(() => ConnectAndReadAsync(_cancellationTokenSource.Token));
        }
    }

    private async Task ConnectAndReadAsync(CancellationToken cancellationToken)
    {
        int attempt = 0;
        int[] backoffMs = { 0, 1000, 2000, 4000, 8000, 15000, 30000 }; // First attempt immediate, then exponential

        while (!cancellationToken.IsCancellationRequested)
        {
            // Wait before attempting (except first time)
            if (attempt > 0)
            {
                int delay = backoffMs[Math.Min(attempt, backoffMs.Length - 1)];

                try
                {
                    await Task.Delay(delay, cancellationToken);
                }
                catch (OperationCanceledException)
                {
                    break;
                }
            }

            try
            {
                // Clean up and create new pipe client
                lock (_pipeLock)
                {
                    CleanupPipeResources();
                    _pipeClient = new NamedPipeClientStream(
                        ".",
                        PipeName,
                        PipeDirection.InOut,
                        PipeOptions.Asynchronous
                    );
                }

                // Connect with timeout (blocking call, but inside Task.Run)
                _pipeClient.Connect(5000);

                lock (_pipeLock)
                {
                    if (!_pipeClient.IsConnected)
                    {
                        continue;
                    }

                    // Use UTF8Encoding(false) to disable BOM - Go's JSON parser doesn't expect it
                    var utf8NoBom = new UTF8Encoding(false);
                    _pipeWriter = new StreamWriter(_pipeClient, utf8NoBom) { AutoFlush = true };
                    _pipeReader = new StreamReader(_pipeClient, utf8NoBom);
                }

                // Reset backoff after successful connection
                attempt = 0;

                // Announce what this plugin supports before anything else, so
                // Core knows whether it can ask for a stop.
                Log($"Connected to Zaparoo (plugin {PluginVersion}, protocol {ProtocolVersion})");
                SendEvent(new HelloEvent
                {
                    PluginVersion = PluginVersion,
                    ProtocolVersion = ProtocolVersion
                });

                // Read commands until pipe breaks or cancelled
                await ReadCommandsAsync(cancellationToken);
            }
            catch (OperationCanceledException)
            {
                break;
            }
            catch (Exception)
            {
                // Connection failed, will retry
            }

            // Clean up after connection loss
            lock (_pipeLock)
            {
                CleanupPipeResources();
            }

            attempt++;
        }
    }

    private void DisconnectFromPipe()
    {
        // Cancel tasks first (while holding lock)
        lock (_pipeLock)
        {
            _cancellationTokenSource?.Cancel();
        }

        // Wait for tasks to finish OUTSIDE the lock to avoid deadlock
        try
        {
            _connectionTask?.Wait(1000);
        }
        catch
        {
            // Ignore cancellation/timeout errors
        }

        // Final cleanup with lock
        lock (_pipeLock)
        {
            CleanupPipeResources();
            _cancellationTokenSource?.Dispose();
            _cancellationTokenSource = null;
            _connectionTask = null;
        }
    }

    private void CleanupPipeResources()
    {
        // Must be called with _pipeLock held
        _pipeWriter?.Dispose();
        _pipeWriter = null;

        _pipeReader?.Dispose();
        _pipeReader = null;

        _pipeClient?.Dispose();
        _pipeClient = null;
    }

    private void SendEvent<T>(T eventData) where T : class
    {
        bool needsReconnect = false;

        lock (_pipeLock)
        {
            if (_pipeWriter == null || _pipeClient?.IsConnected != true)
            {
                return; // Pipe not connected, ignore
            }

            try
            {
                string json = JsonSerializer.Serialize(eventData);
                _pipeWriter.WriteLine(json);
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"Zaparoo plugin: Failed to send event: {ex.Message}");
                // Pipe broken, mark for reconnection
                needsReconnect = true;
                CleanupPipeResources();
            }
        }

        // Trigger reconnect after releasing lock
        if (needsReconnect && !_isShuttingDown)
        {
            StartConnectionTask();
        }
    }

    private async Task ReadCommandsAsync(CancellationToken cancellationToken)
    {
        try
        {
            while (!cancellationToken.IsCancellationRequested)
            {
                StreamReader? reader;
                lock (_pipeLock)
                {
                    reader = _pipeReader;
                }

                if (reader == null)
                {
                    break;
                }

                // ReadLineAsync doesn't support CancellationToken on .NET Framework
                // Use Task.WhenAny pattern for cancellation
                var readTask = reader.ReadLineAsync();
                var tcs = new TaskCompletionSource<bool>();

                using (cancellationToken.Register(() => tcs.TrySetCanceled()))
                {
                    var completedTask = await Task.WhenAny(readTask, tcs.Task);
                    if (completedTask != readTask)
                    {
                        // Cancellation occurred
                        break;
                    }
                }

                string? line = await readTask;
                if (line == null)
                {
                    break; // Pipe closed
                }

                HandleCommand(line);
            }
        }
        catch (Exception)
        {
            // Ignore errors, connection will be retried
        }
    }

    private void HandleCommand(string json)
    {
        try
        {
            var command = JsonSerializer.Deserialize<PluginCommand>(json);
            if (command == null)
            {
                return;
            }

            string commandName = command.Command ?? string.Empty;
            switch (commandName.ToLowerInvariant())
            {
                case "launch":
                    if (!string.IsNullOrEmpty(command.Id))
                    {
                        LaunchGameById(command.Id);
                    }
                    else
                    {
                        SendCommandError("Launch", "Missing required field 'Id' for launch");
                    }
                    break;

                case "showplatforms":
                    ShowPlatforms();
                    break;

                case "showallgames":
                    ShowAllGames();
                    break;

                case "showplatform":
                    if (!string.IsNullOrEmpty(command.Platform))
                    {
                        ShowPlatform(command.Platform);
                    }
                    else
                    {
                        SendCommandError("ShowPlatform", "Missing required field 'Platform' for showplatform");
                    }
                    break;

                case "showplaylist":
                    if (!string.IsNullOrEmpty(command.Playlist))
                    {
                        ShowPlaylist(command.Playlist);
                    }
                    else
                    {
                        SendCommandError("ShowPlaylist", "Missing required field 'Playlist' for showplaylist");
                    }
                    break;

                case "search":
                    if (!string.IsNullOrEmpty(command.Query))
                    {
                        Search(command.Query);
                    }
                    else
                    {
                        SendCommandError("Search", "Missing required field 'Query' for search");
                    }
                    break;

                case "openmanual":
                    OpenManual(command.Id);
                    break;

                case "getplatforms":
                    SendPlatformsList();
                    break;

                case "getgames":
                    SendGamesList();
                    break;

                case "getgamesforplatform":
                    if (!string.IsNullOrEmpty(command.Platform))
                    {
                        SendGamesForPlatform(command.Platform);
                    }
                    else
                    {
                        SendCommandError("GetGamesForPlatform", "Missing required field 'Platform' for getgamesforplatform");
                    }
                    break;

                case "stop":
                    // Stopping waits for the process to exit, so it must not
                    // run on the pipe read loop or no other command from Core
                    // would be served meanwhile.
                    var stopId = command.Id;
                    System.Threading.Tasks.Task.Run(() => StopRunningGame(stopId));
                    break;

                case "ping":
                    // Heartbeat to keep connection alive - no action needed
                    break;

                default:
                    SendCommandError(commandName, "Unknown command");
                    break;
            }
        }
        catch (Exception ex)
        {
            Log($"Command failed: {ex.Message}");
            SendCommandError("Command", ex.Message);
        }
    }

    private void LaunchGameById(string gameId)
    {
        try
        {
            if (IsGameRunning())
            {
                Log($"Ignoring launch for {gameId}: a game is already running");
                SendCommandError("Launch", "A game is already running");
                return;
            }

            // First try to find as a regular game
            var game = PluginHelper.DataManager.GetGameById(gameId);
            if (game != null)
            {
                LaunchGame(game, null);
                return;
            }

            // Try to find as an additional application (merged games, secondary discs)
            var (parentGame, additionalApp) = FindAdditionalApplicationById(gameId);
            if (additionalApp != null && parentGame != null)
            {
                LaunchGame(parentGame, additionalApp);
                return;
            }

            Log($"Game or additional app not found: {gameId}");
            SendCommandError("Launch", $"Game or additional app not found: {gameId}");
        }
        catch (Exception ex)
        {
            Log($"Failed to launch game {gameId}: {ex.Message}");
            SendCommandError("Launch", ex.Message);
        }
    }

    private void LaunchGame(IGame game, IAdditionalApplication? app)
    {
        InvokeOnUiThread(() =>
        {
            if (_isBigBoxRunning || PluginHelper.StateManager?.IsBigBox == true)
            {
                EnsureForegroundHook();
                BeginPendingLaunch(game, app, navigateAfterLaunch: true);

                try
                {
                    Log($"Launching BigBox game: {game.Title} ({game.Id})");
                    PluginHelper.BigBoxMainViewModel.PlayGame(game, app, null, null);
                }
                catch
                {
                    ClearPendingLaunch(game, app);
                    throw;
                }
                return;
            }

            BeginPendingLaunch(game, app, navigateAfterLaunch: false);
            try
            {
                Log($"Launching LaunchBox game: {game.Title} ({game.Id})");
                PluginHelper.LaunchBoxMainViewModel.PlayGame(game, app, null, null);
            }
            catch
            {
                ClearPendingLaunch(game, app);
                throw;
            }
        }, "Launch game");
    }

    private static void InvokeOnUiThread(Action action, string description)
    {
        CaptureApplicationContext();
        Dispatcher? dispatcher = _uiDispatcher ?? System.Windows.Application.Current?.Dispatcher;
        if (dispatcher == null)
        {
            Log($"Cannot run UI action without dispatcher: {description}");
            return;
        }

        if (dispatcher.CheckAccess())
        {
            action();
            return;
        }

        dispatcher.Invoke(action);
    }

    private static bool IsGameRunning()
    {
        lock (_stateLock)
        {
            return _currentGame != null || _pendingLaunchGameId != null;
        }
    }

    private static bool MatchesPendingLaunch(IGame game, IAdditionalApplication? app)
    {
        return _pendingLaunchGameId == game.Id && _pendingLaunchAdditionalAppId == app?.Id;
    }

    private static void ClearPendingLaunchState()
    {
        lock (_stateLock)
        {
            _pendingLaunchGameId = null;
            _pendingLaunchAdditionalAppId = null;
            _pendingLaunchShouldNavigate = false;
            _shouldNavigateBackAfterLaunch = false;
            _shouldShowGameAfterFocusLoss = false;
            _currentGame = null;
            _currentAdditionalApp = null;
        }
    }

    private static void BeginPendingLaunch(IGame game, IAdditionalApplication? app, bool navigateAfterLaunch)
    {
        lock (_stateLock)
        {
            _pendingLaunchGameId = game.Id;
            _pendingLaunchAdditionalAppId = app?.Id;
            _pendingLaunchShouldNavigate = navigateAfterLaunch;
            _shouldNavigateBackAfterLaunch = false;
            _shouldShowGameAfterFocusLoss = false;
        }

        _ = Task.Run(async () =>
        {
            await Task.Delay(TimeSpan.FromSeconds(30));
            lock (_stateLock)
            {
                if (_pendingLaunchGameId == game.Id && _pendingLaunchAdditionalAppId == app?.Id)
                {
                    Log($"Clearing stale pending launch: {game.Title} ({game.Id})");
                    _pendingLaunchGameId = null;
                    _pendingLaunchAdditionalAppId = null;
                    _pendingLaunchShouldNavigate = false;
                    _shouldNavigateBackAfterLaunch = false;
                    _shouldShowGameAfterFocusLoss = false;
                }
            }
        });
    }

    private static void ClearPendingLaunch(IGame game, IAdditionalApplication? app)
    {
        lock (_stateLock)
        {
            if (_pendingLaunchGameId == game.Id && _pendingLaunchAdditionalAppId == app?.Id)
            {
                _pendingLaunchGameId = null;
                _pendingLaunchAdditionalAppId = null;
                _pendingLaunchShouldNavigate = false;
                _shouldNavigateBackAfterLaunch = false;
                _shouldShowGameAfterFocusLoss = false;
            }
        }
    }

    private static void ClearCurrentGameIfSelectionChanged()
    {
        // CLI Launcher uses selection changes to repair stale in-game state, but Zaparoo relies on
        // OnGameExited to emit MediaStopped and to keep duplicate launches blocked while a game runs.
        // Leave running state intact here; stale pending launches are handled by their timeout.
    }

    private static void EnsureForegroundHook()
    {
        lock (_stateLock)
        {
            if (_foregroundHook != 0)
            {
                return;
            }

            try
            {
                _foregroundDelegate = OnForegroundChanged;
                _foregroundHook = SetWinEventHook(
                    EventSystemForeground,
                    EventSystemForeground,
                    0,
                    _foregroundDelegate,
                    0,
                    0,
                    WinEventOutOfContext
                );

                if (_foregroundHook == 0)
                {
                    _foregroundDelegate = null;
                    Log("Failed to install foreground hook");
                }
            }
            catch (Exception ex)
            {
                _foregroundDelegate = null;
                _foregroundHook = 0;
                Log($"Failed to install foreground hook: {ex.Message}");
            }
        }
    }

    private static void StopForegroundHook()
    {
        lock (_stateLock)
        {
            try
            {
                if (_foregroundHook != 0)
                {
                    UnhookWinEvent(_foregroundHook);
                    _foregroundHook = 0;
                }

                _foregroundDelegate = null;
            }
            catch (Exception ex)
            {
                Log($"Failed to remove foreground hook: {ex.Message}");
            }
        }
    }

    private static void OnForegroundChanged(
        nint hWinEventHook,
        uint eventType,
        nint hwnd,
        int idObject,
        int idChild,
        uint dwEventThread,
        uint dwmsEventTime
    )
    {
        if (IsLaunchBoxOrBigBoxActiveWindow())
        {
            return;
        }

        IGame? gameToShow = null;
        lock (_stateLock)
        {
            if (_shouldShowGameAfterFocusLoss && _currentGame != null)
            {
                _shouldShowGameAfterFocusLoss = false;
                gameToShow = _currentGame;
            }
        }

        if (gameToShow == null)
        {
            return;
        }

        InvokeOnUiThread(() =>
        {
            Log($"Showing launched game after focus loss: {gameToShow.Title}");
            PluginHelper.BigBoxMainViewModel.ShowGame(gameToShow, FilterType.Local);
        }, "Show launched BigBox game");
    }

    private static bool IsLaunchBoxOrBigBoxActiveWindow()
    {
        try
        {
            string title = GetActiveWindowTitle();
            return title.Equals("launchbox", StringComparison.OrdinalIgnoreCase) ||
                   title.Equals("launchbox big box", StringComparison.OrdinalIgnoreCase);
        }
        catch
        {
            return false;
        }
    }

    private static string GetActiveWindowTitle()
    {
        nint window = GetForegroundWindow();
        if (window == 0)
        {
            return string.Empty;
        }

        var title = new StringBuilder(256);
        return GetWindowText(window, title, title.Capacity) > 0 ? title.ToString() : string.Empty;
    }

    private static void NavigateBackAfterZaparooLaunch()
    {
        bool shouldNavigate;
        lock (_stateLock)
        {
            shouldNavigate = _shouldNavigateBackAfterLaunch;
            _shouldNavigateBackAfterLaunch = false;
            _shouldShowGameAfterFocusLoss = false;
        }

        if (!shouldNavigate || !IsBigBox())
        {
            return;
        }

        InvokeOnUiThread(() =>
        {
            if (!SendBigBoxBackKey())
            {
                Log("Falling back to ShowAllGames after BigBox Back was unavailable");
                PluginHelper.BigBoxMainViewModel.ShowAllGames();
            }
        }, "Navigate back after BigBox launch");
    }

    private static bool SendBigBoxBackKey()
    {
        string? setting = ReadBigBoxSetting("KeyboardBack");
        if (string.IsNullOrWhiteSpace(setting) || setting == "0")
        {
            return false;
        }

        if (!int.TryParse(setting, out int keyValue))
        {
            Log($"Invalid KeyboardBack setting: {setting}");
            return false;
        }

        byte virtualKey = (byte)KeyInterop.VirtualKeyFromKey((Key)keyValue);
        if (virtualKey == 0)
        {
            return false;
        }

        if (!IsLaunchBoxOrBigBoxActiveWindow())
        {
            Log("Skipping BigBox Back key because BigBox is not foreground");
            return false;
        }

        Log($"Navigating BigBox back with virtual key: {virtualKey}");
        keybd_event(virtualKey, 0, 0, 0);
        keybd_event(virtualKey, 0, 2, 0);
        return true;
    }

    private static string? ReadBigBoxSetting(string name)
    {
        try
        {
            CaptureApplicationContext();
            if (string.IsNullOrEmpty(_launchBoxRoot))
            {
                return null;
            }

            string path = Path.Combine(_launchBoxRoot, "Data", "BigBoxSettings.xml");
            if (!File.Exists(path))
            {
                return null;
            }

            XDocument doc = XDocument.Load(path);
            return doc.XPathSelectElement($"/LaunchBox/BigBoxSettings/{name}")?.Value;
        }
        catch (Exception ex)
        {
            Log($"Failed to read BigBox setting {name}: {ex.Message}");
            return null;
        }
    }

    private (IGame?, IAdditionalApplication?) FindAdditionalApplicationById(string id)
    {
        foreach (var game in PluginHelper.DataManager.GetAllGames())
        {
            foreach (var app in game.GetAllAdditionalApplications())
            {
                if (app.Id == id)
                {
                    return (game, app);
                }
            }
        }
        return (null, null);
    }

    private void ShowPlatforms()
    {
        InvokeOnUiThread(() =>
        {
            if (!IsBigBox())
            {
                SendCommandError("ShowPlatforms", "Platform navigation only works in BigBox");
                return;
            }

            if (IsGameRunning())
            {
                SendCommandError("ShowPlatforms", "A game is already running");
                return;
            }

            PluginHelper.BigBoxMainViewModel.ShowPlatforms();
        }, "Show BigBox platforms");
    }

    private void ShowAllGames()
    {
        InvokeOnUiThread(() =>
        {
            if (!IsBigBox())
            {
                SendCommandError("ShowAllGames", "All Games navigation only works in BigBox");
                return;
            }

            if (IsGameRunning())
            {
                SendCommandError("ShowAllGames", "A game is already running");
                return;
            }

            PluginHelper.BigBoxMainViewModel.ShowAllGames();
        }, "Show BigBox all games");
    }

    private void ShowPlatform(string platformName)
    {
        InvokeOnUiThread(() =>
        {
            if (!IsBigBox())
            {
                SendCommandError("ShowPlatform", "Platform navigation only works in BigBox");
                return;
            }

            if (IsGameRunning())
            {
                SendCommandError("ShowPlatform", "A game is already running");
                return;
            }

            IPlatform? platform = PluginHelper.DataManager.GetPlatformByName(platformName);
            if (platform == null)
            {
                SendCommandError("ShowPlatform", $"Platform not found: {platformName}");
                return;
            }

            PluginHelper.BigBoxMainViewModel.ShowGames(platform);
        }, "Show BigBox platform");
    }

    private void ShowPlaylist(string playlistName)
    {
        InvokeOnUiThread(() =>
        {
            if (!IsBigBox())
            {
                SendCommandError("ShowPlaylist", "Playlist navigation only works in BigBox");
                return;
            }

            if (IsGameRunning())
            {
                SendCommandError("ShowPlaylist", "A game is already running");
                return;
            }

            IPlaylist? playlist = PluginHelper.DataManager.GetAllPlaylists()
                .FirstOrDefault(p => p.Name.Equals(playlistName, StringComparison.OrdinalIgnoreCase));
            if (playlist == null)
            {
                SendCommandError("ShowPlaylist", $"Playlist not found: {playlistName}");
                return;
            }

            PluginHelper.BigBoxMainViewModel.ShowGames(FilterType.PlatformOrCategoryOrPlaylist, playlist.Name);
        }, "Show BigBox playlist");
    }

    private void Search(string query)
    {
        InvokeOnUiThread(() =>
        {
            if (!IsBigBox())
            {
                SendCommandError("Search", "Search command only works in BigBox");
                return;
            }

            if (IsGameRunning())
            {
                SendCommandError("Search", "A game is already running");
                return;
            }

            PluginHelper.BigBoxMainViewModel.Search(query);
        }, "Search BigBox");
    }

    private void OpenManual(string? gameId)
    {
        InvokeOnUiThread(() =>
        {
            IGame? game = null;
            if (!string.IsNullOrEmpty(gameId))
            {
                game = PluginHelper.DataManager.GetGameById(gameId);
            }

            if (game == null)
            {
                lock (_stateLock)
                {
                    game = _currentGame;
                }
            }

            if (game == null)
            {
                IGame[]? selectedGames = PluginHelper.StateManager?.GetAllSelectedGames();
                game = selectedGames is { Length: > 0 } ? selectedGames[0] : null;
            }

            if (game == null)
            {
                SendCommandError("OpenManual", "No game is selected or running");
                return;
            }

            string? result = game.OpenManual();
            if (!string.IsNullOrEmpty(result))
            {
                Log($"OpenManual result for {game.Title}: {result}");
            }
        }, "Open manual");
    }

    private static bool IsBigBox()
    {
        return _isBigBoxRunning || PluginHelper.StateManager?.IsBigBox == true;
    }

    private void SendPlatformsList()
    {
        try
        {
            var platforms = PluginHelper.DataManager.GetAllPlatforms();
            var response = new PlatformsResponseEvent();

            foreach (var platform in platforms)
            {
                response.Platforms.Add(new PlatformInfo
                {
                    Name = platform.Name,
                    // Fall back to Name if ScrapeAs is not set
                    ScrapeAs = string.IsNullOrEmpty(platform.ScrapeAs) ? platform.Name : platform.ScrapeAs
                });
            }

            SendEvent(response);
        }
        catch (Exception)
        {
            // Ignore errors - platforms won't be sent
        }
    }

    private void SendGamesList()
    {
        try
        {
            var allGames = PluginHelper.DataManager.GetAllGames();
            var response = new GamesResponseEvent();

            foreach (var game in allGames)
            {
                var gameInfo = new GameInfo
                {
                    Id = game.Id,
                    Title = game.Title,
                    Platform = game.Platform
                };

                // Include additional applications (secondary discs, merged games, etc.)
                var additionalApps = game.GetAllAdditionalApplications();
                if (additionalApps != null)
                {
                    foreach (var app in additionalApps)
                    {
                        gameInfo.AdditionalApps.Add(new AdditionalAppInfo
                        {
                            Id = app.Id,
                            Name = app.Name
                        });
                    }
                }

                response.Games.Add(gameInfo);
            }

            SendEvent(response);
        }
        catch (Exception ex)
        {
            System.Diagnostics.Debug.WriteLine($"Zaparoo plugin: Failed to get games list: {ex.Message}");
        }
    }

    private void SendGamesForPlatform(string platform)
    {
        try
        {
            var allGames = PluginHelper.DataManager.GetAllGames()
                .Where(g => g.Platform == platform);

            var response = new GamesResponseEvent { Platform = platform };

            foreach (var game in allGames)
            {
                var gameInfo = new GameInfo
                {
                    Id = game.Id,
                    Title = game.Title,
                    Platform = game.Platform
                };

                // Include additional applications (secondary discs, merged games, etc.)
                var additionalApps = game.GetAllAdditionalApplications();
                if (additionalApps != null)
                {
                    foreach (var app in additionalApps)
                    {
                        gameInfo.AdditionalApps.Add(new AdditionalAppInfo
                        {
                            Id = app.Id,
                            Name = app.Name
                        });
                    }
                }

                response.Games.Add(gameInfo);
            }

            SendEvent(response);
        }
        catch (Exception ex)
        {
            System.Diagnostics.Debug.WriteLine($"Zaparoo plugin: Failed to get games for {platform}: {ex.Message}");
            SendEvent(new GamesResponseEvent { Platform = platform, Error = ex.Message });
        }
    }

    private void SendCommandError(string command, string message)
    {
        Log($"{command} error: {message}");
        SendEvent(new ErrorEvent
        {
            Command = command,
            Error = message
        });
    }

    #endregion

    #region Message Types

    private class GameStartedEvent
    {
        public string Event { get; set; } = string.Empty;
        public string Id { get; set; } = string.Empty;
        public string Title { get; set; } = string.Empty;
        public string Platform { get; set; } = string.Empty;
        public string ApplicationPath { get; set; } = string.Empty;
        public int Pid { get; set; }
    }

    private class GameProcessEvent
    {
        public string Event { get; set; } = "MediaProcess";
        public string Id { get; set; } = string.Empty;
        public int Pid { get; set; }
    }

    private class HelloEvent
    {
        public string Event { get; set; } = "Hello";
        public string PluginVersion { get; set; } = string.Empty;
        public int ProtocolVersion { get; set; }
    }

    private class StopResultEvent
    {
        public string Event { get; set; } = "MediaStopResult";
        public string Id { get; set; } = string.Empty;
        public string Status { get; set; } = string.Empty;
        public string Error { get; set; } = string.Empty;
    }

    private class GameExitedEvent
    {
        public string Event { get; set; } = string.Empty;
        public string Id { get; set; } = string.Empty;
        public string Title { get; set; } = string.Empty;
    }

    private class WriteRequestEvent
    {
        public string Event { get; set; } = string.Empty;
        public string Id { get; set; } = string.Empty;
        public string Title { get; set; } = string.Empty;
        public string Platform { get; set; } = string.Empty;
    }

    private class PluginCommand
    {
        public string? Command { get; set; }
        public string? Id { get; set; }
        public string? Platform { get; set; }
        public string? Playlist { get; set; }
        public string? Query { get; set; }
    }

    private class ErrorEvent
    {
        public string Event { get; set; } = "Error";
        public string Command { get; set; } = string.Empty;
        public string Error { get; set; } = string.Empty;
    }

    private class PlatformInfo
    {
        public string Name { get; set; } = string.Empty;
        public string ScrapeAs { get; set; } = string.Empty;
    }

    private class PlatformsResponseEvent
    {
        public string Event { get; set; } = "Platforms";
        public List<PlatformInfo> Platforms { get; set; } = new();
    }

    private class AdditionalAppInfo
    {
        public string Id { get; set; } = string.Empty;
        public string Name { get; set; } = string.Empty;
    }

    private class GameInfo
    {
        public string Id { get; set; } = string.Empty;
        public string Title { get; set; } = string.Empty;
        public string Platform { get; set; } = string.Empty;
        public List<AdditionalAppInfo> AdditionalApps { get; set; } = new();
    }

    private class GamesResponseEvent
    {
        public string Event { get; set; } = "Games";
        public string Platform { get; set; } = string.Empty;
        public string? Error { get; set; }
        public List<GameInfo> Games { get; set; } = new();
    }

    #endregion
}
