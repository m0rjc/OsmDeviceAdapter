import { useState, useEffect } from 'react';
import {
    useAppDispatch,
    useAppSelector,
    selectAllScoreboards,
    selectScoreboardsLoadState,
    fetchScoreboards,
    sendTimerCommand,
} from '../../state';
import { useToast } from '../../hooks';

type TimerState = 'idle' | 'running' | 'paused';

export function TimerControlPage() {
    const dispatch = useAppDispatch();
    const { showToast } = useToast();

    const scoreboards = useAppSelector(selectAllScoreboards);
    const loadState = useAppSelector(selectScoreboardsLoadState);

    const [selectedDevice, setSelectedDevice] = useState<string>('');
    const [durationMinutes, setDurationMinutes] = useState<number>(5);
    const [durationSeconds, setDurationSeconds] = useState<number>(0);
    const [timerState, setTimerState] = useState<TimerState>('idle');

    useEffect(() => {
        if (loadState === 'uninitialized') {
            dispatch(fetchScoreboards());
        }
    }, [loadState, dispatch]);

    useEffect(() => {
        if (!selectedDevice && scoreboards.length > 0) {
            setSelectedDevice(scoreboards[0].deviceCodePrefix);
        }
    }, [scoreboards, selectedDevice]);

    const sendCommand = async (command: 'start' | 'pause' | 'resume' | 'reset', duration?: number) => {
        if (!selectedDevice) {
            showToast('error', 'Please select a scoreboard device');
            return;
        }
        try {
            await dispatch(sendTimerCommand({ deviceCodePrefix: selectedDevice, command, duration })).unwrap();
        } catch (e) {
            showToast('error', e instanceof Error ? e.message : 'Failed to send timer command');
        }
    };

    const handleStart = async () => {
        const duration = durationMinutes * 60 + durationSeconds;
        if (duration <= 0) {
            showToast('error', 'Please set a duration greater than 0');
            return;
        }
        await sendCommand('start', duration);
        setTimerState('running');
    };

    const handlePauseResume = async () => {
        if (timerState === 'running') {
            await sendCommand('pause');
            setTimerState('paused');
        } else if (timerState === 'paused') {
            await sendCommand('resume');
            setTimerState('running');
        }
    };

    const handleReset = async () => {
        await sendCommand('reset');
        setTimerState('idle');
    };

    return (
        <div className="settings-section">
            <h3 className="settings-section-title">Countdown Timer</h3>
            <p className="settings-section-description">
                Send timer commands to a connected scoreboard device.
            </p>

            <div className="timer-controls">
                <div className="form-group">
                    <label className="form-label">Scoreboard Device</label>
                    <select
                        className="scoreboard-section-select"
                        value={selectedDevice}
                        onChange={(e) => {
                            setSelectedDevice(e.target.value);
                            setTimerState('idle');
                        }}
                        disabled={scoreboards.length === 0}
                    >
                        {scoreboards.length === 0 ? (
                            <option value="">No devices available</option>
                        ) : (
                            scoreboards.map((board) => (
                                <option key={board.deviceCodePrefix} value={board.deviceCodePrefix}>
                                    {board.deviceCodePrefix}... {board.sectionName ? `(${board.sectionName})` : ''}
                                </option>
                            ))
                        )}
                    </select>
                </div>

                <div className="form-group">
                    <label className="form-label">Duration</label>
                    <div className="timer-duration-inputs">
                        <input
                            type="number"
                            className="timer-duration-input"
                            min={0}
                            max={99}
                            value={durationMinutes}
                            onChange={(e) => setDurationMinutes(Math.max(0, parseInt(e.target.value, 10) || 0))}
                            disabled={timerState !== 'idle'}
                            aria-label="Minutes"
                        />
                        <span className="timer-duration-separator">:</span>
                        <input
                            type="number"
                            className="timer-duration-input"
                            min={0}
                            max={59}
                            value={durationSeconds}
                            onChange={(e) => setDurationSeconds(Math.min(59, Math.max(0, parseInt(e.target.value, 10) || 0)))}
                            disabled={timerState !== 'idle'}
                            aria-label="Seconds"
                        />
                        <span className="timer-duration-label">mm:ss</span>
                    </div>
                </div>

                <div className="timer-buttons">
                    {timerState === 'idle' ? (
                        <button
                            className="btn btn-primary"
                            onClick={handleStart}
                            disabled={!selectedDevice}
                        >
                            Start
                        </button>
                    ) : (
                        <>
                            <button
                                className="btn btn-secondary"
                                onClick={handlePauseResume}
                            >
                                {timerState === 'running' ? 'Pause' : 'Resume'}
                            </button>
                            <button
                                className="btn btn-danger"
                                onClick={handleReset}
                            >
                                Reset
                            </button>
                        </>
                    )}
                </div>
            </div>
        </div>
    );
}
