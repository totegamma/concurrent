import { Backdrop, Box, CircularProgress, Divider, Paper, TextField, Typography } from '@mui/material'
import type { RJSFSchema } from '@rjsf/utils'
import Form from '@rjsf/mui'
import validator from '@rjsf/validator-ajv8'
import { useSearchParams } from 'react-router-dom'
import React from 'react'
import { DomainProfile } from '../model'

const schema: RJSFSchema = {
    description: '情報はトラブル対応や本人確認にのみ用いられ、このホストの管理人以外には公開されません。',
    type: 'object',
    required: ['name', 'email', 'consent'],
    properties: {
        name: { type: 'string', title: '名前', description: 'ご連絡が必要になった場合に用いる宛名　ハンドルネーム推奨' },
        email: { type: 'string', title: 'メールアドレス', description: '最終的なご連絡先' },
        social: { type: 'string', title: 'その他連絡先', description: 'TwitterやMisskeyやMastodonなどの連絡先' },
        consent: { type: 'boolean', title: 'ルールを理解しました', default: null, enum: [null, true]}
    },
}

export const Register = ({profile}: {profile: DomainProfile | null}): JSX.Element => {
    const [searchParams] = useSearchParams()
    const [loading, setLoading] = React.useState(false);
    const [success, setSuccess] = React.useState(false);
    const [inviteCode, setInviteCode] = React.useState<string>("");

    const token = searchParams.get('token')
    let ccaddr = ""
    if (token) {
        const split = token.split('.')
        const encoded = split[1]
        const payload = window.atob(
            encoded.replace('-', '+').replace('_', '/') + '=='.slice((2 - encoded.length * 3) & 3)
        )
        const claims = JSON.parse(payload)
        ccaddr = claims.iss
    }

    const register = (meta: any): void => {
        if (!token) return
        setLoading(true)
        const requestOptions = {
            method: 'POST',
            headers: {
                'content-type': 'application/json',
                'authentication': 'Bearer ' + token
            },
            body: JSON.stringify({
                ccaddr,
                meta: JSON.stringify(meta),
                token: inviteCode
            })
        }

        fetch(
            '/api/v1/entity',
            requestOptions
        )
            .then(async (res) => await res.json())
            .then((data) => {
                console.log(data)
                if (data.error) {
                    alert(data.error)
                    setLoading(false)
                    return
                }
                setLoading(false)
                setSuccess(true)
            })
    }


    return (
        <>
            <Backdrop open={loading} sx={{zIndex: 1000}}>
                <CircularProgress color="inherit" />
            </Backdrop>
            <Box
                display='flex'
                flexDirection='column'
                gap='20px'
            >
                <Box>
                    <Typography variant="h4">Registration</Typography>
                    <Typography>for {ccaddr}</Typography>
                </Box>
                <Divider />
                <Box>
                    <Typography variant="h5">{profile?.nickname}</Typography>
                    <Typography>{profile?.description}</Typography>
                </Box>
                <Box>
                    <Typography variant="h5">Rules</Typography>
                    <Paper
                        variant="outlined"
                        sx={{
                            px: '20px',
                        }}
                    >
                        <pre>
                            {profile?.rules}
                        </pre>
                    </Paper>
                </Box>
            </Box>
            {profile?.registration === 'close' ?
                <Typography>登録は現在受け付けていません</Typography>
            : (
            success ?
                <>登録完了</>
            :
                <>
                {profile?.registration === 'invite' &&
                <TextField
                    label="招待コード"
                    variant="outlined"
                    value={inviteCode}
                    onChange={(e) => setInviteCode(e.target.value)}
                    sx={{my: '20px'}}
                    required
                />
                }
                <Form
                    disabled={loading}
                    schema={schema}
                    validator={validator}
                    onSubmit={(e) => {register(e.formData)}}
                />
                </>
            )}
        </>
    )
}
