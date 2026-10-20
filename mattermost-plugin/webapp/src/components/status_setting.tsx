import manifest from 'manifest';
import React, {useCallback, useEffect, useState} from 'react';

type SettingStatus = {
    key: string;
    source: 'environment' | 'settings' | 'empty';
    valid: boolean;
};

type Status = {
    settings: SettingStatus[];
    odoo: {
        reachable: boolean;
        version?: string;
        webhook_configured: boolean;
        error?: string;
    };
};

const NAMES: Record<string, string> = {
    OdooURL: 'Odoo URL',
    PluginKey: 'Plugin signing key',
    WebhookKey: 'Odoo webhook key',
    PreviousWebhookKey: 'Previous Odoo webhook key',
};

const SOURCES: Record<SettingStatus['source'], string> = {
    environment: 'From the environment. The field below is not used.',
    settings: 'From the field below.',
    empty: 'Not set.',
};

const cell: React.CSSProperties = {padding: '4px 16px 4px 0', verticalAlign: 'top'};

function settingText(setting: SettingStatus) {
    if (setting.source === 'empty') {
        return setting.key === 'PreviousWebhookKey' ? 'Not set. This is normal outside a key rotation.' : 'Not set.';
    }
    const text = SOURCES[setting.source];
    return setting.valid ? text : `${text} The value is not valid.`;
}

// StatusSetting shows where each setting comes from and whether Odoo answers.
// It never shows a key.
export default function StatusSetting() {
    const [status, setStatus] = useState<Status | null>(null);
    const [error, setError] = useState('');

    const load = useCallback(async () => {
        setError('');
        try {
            const basename = (window as unknown as {basename?: string}).basename || '';
            const response = await fetch(`${basename}/plugins/${manifest.id}/api/v1/status`, {
                credentials: 'same-origin',
                headers: {'X-Requested-With': 'XMLHttpRequest'},
            });
            if (!response.ok) {
                throw new Error(`The plugin answered ${response.status}.`);
            }
            setStatus(await response.json());
        } catch (e) {
            setStatus(null);
            setError(e instanceof Error ? e.message : String(e));
        }
    }, []);

    useEffect(() => {
        load();
    }, [load]);

    if (error) {
        return <div className='alert alert-warning'>{error}</div>;
    }
    if (!status) {
        return <div>{'Loading...'}</div>;
    }

    let odoo: string;
    if (status.odoo.reachable) {
        odoo = `Odoo answers. Module version ${status.odoo.version || 'unknown'}. ` +
            (status.odoo.webhook_configured ? 'Odoo can send task changes.' : 'Odoo has no webhook key or no Mattermost URL, so cards do not refresh.');
    } else {
        odoo = status.odoo.error || 'Odoo does not answer.';
    }

    return (
        <div>
            <table>
                <tbody>
                    {status.settings.map((setting) => (
                        <tr key={setting.key}>
                            <td style={cell}><strong>{NAMES[setting.key] || setting.key}</strong></td>
                            <td style={cell}>{settingText(setting)}</td>
                        </tr>
                    ))}
                    <tr>
                        <td style={cell}><strong>{'Odoo'}</strong></td>
                        <td style={cell}>{odoo}</td>
                    </tr>
                </tbody>
            </table>
            <button
                type='button'
                className='btn btn-tertiary btn-sm'
                onClick={load}
            >
                {'Check again'}
            </button>
        </div>
    );
}
