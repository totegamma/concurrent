import { Backdrop, CircularProgress, Typography } from '@mui/material'
import type { RJSFSchema } from '@rjsf/utils'
import Form from '@rjsf/mui'
import validator from '@rjsf/validator-ajv8'
import { useSearchParams } from 'react-router-dom'
import React from 'react'

const schema: RJSFSchema = {
    title: 'Concurrent-<ホスト名> 登録フォーム',
    description: '情報はトラブル対応や本人確認にのみ用いられ、このホストの管理人以外には公開されません。',
    type: 'object',
    required: ['name', 'email', 'consent'],
    properties: {
        name: { type: 'string', title: '名前', description: 'ご連絡が必要になった場合に用いる宛名　ハンドルネーム推奨' },
        email: { type: 'string', title: 'メールアドレス', description: '最終的なご連絡先' },
        social: { type: 'string', title: 'その他連絡先', description: 'TwitterやMisskeyやMastodonなどの連絡先' },
        consent: { type: 'boolean', title: '規約・規範に同意します', default: null, enum: [null, true]}
    },
}

export const Register = (): JSX.Element => {
    const [searchParams] = useSearchParams()
    const [loading, setLoading] = React.useState(false);
    const [success, setSuccess] = React.useState(false);

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
                meta: JSON.stringify(meta)
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
            <Typography variant="h1">Registration</Typography>
            <Typography>for {ccaddr}</Typography>
            {success ?
                <>登録完了</>
            :
                <Form
                    disabled={loading}
                    schema={schema}
                    validator={validator}
                    onSubmit={(e) => {register(e.formData)}}
                />
            }
        </>
    )
}
