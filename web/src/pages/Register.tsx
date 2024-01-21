import { Backdrop, Box, Button, CircularProgress, Divider, Paper, TextField, Typography } from '@mui/material'
import type { RJSFSchema } from '@rjsf/utils'
import Form from '@rjsf/mui'
import validator from '@rjsf/validator-ajv8'
import { useSearchParams } from 'react-router-dom'
import { useState } from 'react'
import { DomainProfile } from '../model'
import { useApi } from '../context/apiContext'
import ReCAPTCHA from "react-google-recaptcha";

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

    const { api } = useApi()

    const [searchParams] = useSearchParams()
    const [loading, setLoading] = useState(false);
    const [success, setSuccess] = useState(false);
    const [inviteCode, setInviteCode] = useState<string>("");
    const [captcha, setCaptcha] = useState<string>("")
    const [formData, setFormData] = useState<any>({})

    const encodedregistration = searchParams.get('registration')
    const registration = encodedregistration ? atob(encodedregistration.replace('-', '+').replace('_', '/')) : null
    const signature = searchParams.get('signature')
    const callback = searchParams.get('callback')
    let ccaddr = ""
    if (registration) {
        const signedObj = JSON.parse(registration)
        ccaddr = signedObj.signer
    }

    console.log('registration', registration)
    console.log('signature', signature)

    const register = (meta: any): void => {
        if (!registration || !signature) return
        setLoading(true)

        api.register(ccaddr, meta, registration, signature, inviteCode, captcha)
        .then(async (res) => await res.json())
        .then((data) => {
            console.log(data)
            setSuccess(true)

            if (callback) {
                setTimeout(() => {
                    window.location.href = callback
                }, 1000)
            }

        }).catch((e) => {
            alert(e)
        }).finally(() => {
            setLoading(false)
        })
    }

    if (!profile) return <>Loading...</>

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
                    <Typography variant="h5">{profile.nickname}</Typography>
                    <Typography>{profile.description}</Typography>
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
                            {profile.rules}
                        </pre>
                    </Paper>
                </Box>
            </Box>
            {profile.registration === 'close' ?
                <Typography>登録は現在受け付けていません</Typography>
            : (
            success ? <>
                <Typography>登録完了</Typography>
                {callback && <Typography>元のページに戻ります...</Typography>}
            </>
            :
                <>
                {profile.registration === 'invite' &&
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
                    formData={formData}
                    onChange={(e) => setFormData(e.formData)}
                >
                    {profile.captchaSiteKey &&
                        <ReCAPTCHA
                          sitekey={profile.captchaSiteKey}
                          onChange={(e) => setCaptcha(e ?? '')}
                        />
                    }
                    <Button
                        type='submit'
                        variant='contained'
                        disabled={(!!profile.captchaSiteKey) && (captcha === "")}
                    >
                        Submit
                    </Button>
                </Form>
                </>

            )}
        </>
    )
}
