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
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
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

    // Static connection state - shared across all plugin instances
    private static NamedPipeClientStream? _pipeClient;
    private static StreamWriter? _pipeWriter;
    private static StreamReader? _pipeReader;
    private static CancellationTokenSource? _cancellationTokenSource;
    private static Task? _connectionTask;
    private static readonly object _pipeLock = new();
    private static bool _isShuttingDown;

    // Instance-specific state
    private IGame? _currentGame;

    public string Name => "Zaparoo LaunchBox Integration";

    // Constructor - try to connect immediately as fallback
    public ZaparooPlugin()
    {
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
        if (eventType == SystemEventTypes.LaunchBoxStartupCompleted ||
            eventType == SystemEventTypes.BigBoxStartupCompleted)
        {
            _isShuttingDown = false;
            StartConnectionTask();
        }
        else if (eventType == SystemEventTypes.LaunchBoxShutdownBeginning ||
                 eventType == SystemEventTypes.BigBoxShutdownBeginning)
        {
            _isShuttingDown = true;
            DisconnectFromPipe();
        }
    }

    #endregion

    #region IGameLaunchingPlugin Implementation

    public void OnBeforeGameLaunching(IGame game, IAdditionalApplication? app, IEmulator? emulator)
    {
        // Track the game that's about to launch
        _currentGame = game;
    }

    public void OnAfterGameLaunched(IGame game, IAdditionalApplication? app, IEmulator? emulator)
    {
        // Send game started event to Zaparoo
        SendEvent(new GameStartedEvent
        {
            Event = "MediaStarted",
            Id = game.Id,
            Title = game.Title,
            Platform = game.Platform,
            ApplicationPath = game.ApplicationPath
        });
    }

    public void OnGameExited()
    {
        // Send game exited event to Zaparoo
        if (_currentGame != null)
        {
            SendEvent(new GameExitedEvent
            {
                Event = "MediaStopped",
                Id = _currentGame.Id,
                Title = _currentGame.Title
            });
            _currentGame = null;
        }
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

            switch (command.Command)
            {
                case "Launch":
                    if (!string.IsNullOrEmpty(command.Id))
                    {
                        LaunchGameById(command.Id);
                    }
                    break;

                case "GetPlatforms":
                    SendPlatformsList();
                    break;

                case "Ping":
                    // Heartbeat to keep connection alive - no action needed
                    break;

                default:
                    break;
            }
        }
        catch (Exception)
        {
            // Ignore command errors
        }
    }

    private void LaunchGameById(string gameId)
    {
        try
        {
            var game = PluginHelper.DataManager.GetGameById(gameId);
            if (game != null)
            {
                // Use the full launch process, not the bare Play() method
                // Pass null for app, emulator, and commandLine to use defaults
                PluginHelper.LaunchBoxMainViewModel.PlayGame(game, null, null, null);
            }
        }
        catch (Exception)
        {
            // Ignore launch errors
        }
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

    #endregion

    #region Message Types

    private class GameStartedEvent
    {
        public string Event { get; set; } = string.Empty;
        public string Id { get; set; } = string.Empty;
        public string Title { get; set; } = string.Empty;
        public string Platform { get; set; } = string.Empty;
        public string ApplicationPath { get; set; } = string.Empty;
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

    #endregion
}
