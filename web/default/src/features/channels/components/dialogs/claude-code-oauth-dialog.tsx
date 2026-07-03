/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useMemo, useState } from 'react'
import { ExternalLink, Copy, Check, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { tryPrettyJson } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/dialog'
import { startClaudeCodeOAuth, completeClaudeCodeOAuth } from '../../api'

type ClaudeCodeOAuthDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onKeyGenerated: (key: string) => void
}

export function ClaudeCodeOAuthDialog({
  open,
  onOpenChange,
  onKeyGenerated,
}: ClaudeCodeOAuthDialogProps) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })

  const [state, setState] = useState({
    authorizeUrl: '',
    callbackUrl: '',
    isStarting: false,
    isCompleting: false,
  })

  useEffect(() => {
    if (!open) {
      setState({
        authorizeUrl: '',
        callbackUrl: '',
        isStarting: false,
        isCompleting: false,
      })
    }
  }, [open])

  const canCopyAuthorizeUrl = Boolean(state.authorizeUrl && !state.isStarting)
  const canComplete = useMemo(
    () => Boolean(state.callbackUrl.trim()) && !state.isCompleting,
    [state.callbackUrl, state.isCompleting]
  )

  const handleStart = async () => {
    setState((prev) => ({ ...prev, isStarting: true }))
    try {
      const res = await startClaudeCodeOAuth()
      if (!res.success) {
        throw new Error(res.message || 'Failed to start OAuth')
      }

      const url = res.data?.authorize_url || ''
      if (!url) {
        throw new Error('Missing authorize_url in response')
      }

      setState((prev) => ({ ...prev, authorizeUrl: url }))
      try {
        window.open(url, '_blank', 'noopener,noreferrer')
        toast.success(t('Opened authorization page'))
      } catch (error) {
        // eslint-disable-next-line no-console
        console.warn('Failed to open authorization page:', error)
        toast.warning(t('Please manually copy and open the authorization link'))
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('OAuth start failed')
      )
    } finally {
      setState((prev) => ({ ...prev, isStarting: false }))
    }
  }

  const handleComplete = async () => {
    if (!state.callbackUrl.trim()) return
    setState((prev) => ({ ...prev, isCompleting: true }))
    try {
      const res = await completeClaudeCodeOAuth(state.callbackUrl.trim())
      if (!res.success) {
        throw new Error(res.message || 'OAuth failed')
      }

      const rawKey = res.data?.key || ''
      if (!rawKey) {
        throw new Error('Missing key in response')
      }

      onKeyGenerated(tryPrettyJson(rawKey))
      toast.success(t('Credential generated'))
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('OAuth failed'))
    } finally {
      setState((prev) => ({ ...prev, isCompleting: false }))
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Claude Code Authorization')}
      description={t(
        'Authorize with your Anthropic account to generate a Claude Code OAuth credential.'
      )}
      contentClassName='sm:max-w-2xl'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={state.isStarting || state.isCompleting}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={handleComplete} disabled={!canComplete}>
            {state.isCompleting && (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            )}
            {state.isCompleting ? t('Generating...') : t('Generate credential')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <Alert>
          <AlertDescription>
            <ol className='list-decimal list-inside space-y-1'>
              <li>{t('Click "Open authorization page" and sign in with your Anthropic account.')}</li>
              <li>{t('After authorization you will be redirected to platform.claude.com — from the callback URL copy the code and state values.')}</li>
              <li>
                {t('Enter them in the field below as')} <code className='bg-muted rounded px-1 text-xs'>code#state</code>
                {', '}{t('e.g.')} <code className='bg-muted rounded px-1 text-xs'>e6EUVi...#c90359...</code>
              </li>
            </ol>
          </AlertDescription>
        </Alert>

        <div className='flex flex-wrap gap-2'>
          <Button onClick={handleStart} disabled={state.isStarting}>
            {state.isStarting ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : (
              <ExternalLink className='mr-2 h-4 w-4' />
            )}
            {t('Open authorization page')}
          </Button>

          <Button
            type='button'
            variant='outline'
            disabled={!canCopyAuthorizeUrl}
            onClick={async () => {
              if (!state.authorizeUrl) return
              await copyToClipboard(state.authorizeUrl)
            }}
            aria-label={t('Copy authorization link')}
            title={t('Copy authorization link')}
          >
            {copiedText === state.authorizeUrl ? (
              <Check className='mr-2 h-4 w-4 text-green-600' />
            ) : (
              <Copy className='mr-2 h-4 w-4' />
            )}
            {t('Copy authorization link')}
          </Button>
        </div>

        <div className='space-y-2'>
          <div className='text-sm font-medium'>{t('Authorization Code')}</div>
          <Input
            value={state.callbackUrl}
            onChange={(e) =>
              setState((prev) => ({ ...prev, callbackUrl: e.target.value }))
            }
            placeholder={t('code#state, e.g. e6EUViZh...#c903598b...')}
            autoComplete='off'
            spellCheck={false}
          />
          <div className='text-muted-foreground text-xs'>
            {t(
              'Tip: From the callback URL https://platform.claude.com/oauth/code/callback?code=CODE&state=STATE, enter CODE#STATE here.'
            )}
          </div>
        </div>
      </div>
    </Dialog>
  )
}
