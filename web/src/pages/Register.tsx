import { Typography } from '@mui/material'
import type { RJSFSchema } from '@rjsf/utils'
import Form from '@rjsf/mui'
import validator from '@rjsf/validator-ajv8'
import { useSearchParams } from 'react-router-dom'

const schema: RJSFSchema = {
    title: 'Concurrent-<ホスト名> 登録フォーム',
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

    const ccaddr = searchParams.get('ccaddr')

    const register = (meta: any): void => {
        const requestOptions = {
            method: 'POST',
            headers: { 'content-type': 'application/json' },
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
            })
    }



    return (
        <>
            <Typography variant="h1">Registration</Typography>
            <Typography>for {ccaddr}</Typography>
            <Form
                schema={schema}
                validator={validator}
                onSubmit={(e) => {register(e.formData)}}
            />
        </>
    )
}
