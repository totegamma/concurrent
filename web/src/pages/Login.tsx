import { useEffect, useState } from 'react';
import { Navigate, useLocation, useSearchParams } from 'react-router-dom'
import { useApi } from '../context/apiContext';

export const Login = (): JSX.Element => {

    const { api, setJWT } = useApi()
    const [searchParams] = useSearchParams()
    const token = searchParams.get('token')
    const location = useLocation()
    const [loading, setLoading] = useState(true)

    useEffect(() => {
        if (!token) return
        const split = token.split('.')
        const encoded = split[1]
        const payload = window.atob(
            encoded.replace('-', '+').replace('_', '/') + '=='.slice((2 - encoded.length * 3) & 3)
        )
        const claims = JSON.parse(payload)
        const ccid = claims.aud

        api.readEntity(ccid).then((entity) => {
            if (!entity) {
                alert("entity not found")
                return
            }
            localStorage.setItem("ENTITY", JSON.stringify(entity))
            setJWT(token)
            setLoading(false)
        })
    }, [token])

    return loading ? (
        token ? (
            <>loading...</>
        ) : (
            <>oops! something went wrong</>
        )
    ) : (
        <Navigate to='/' state={{ from: location }} replace={true} />
    )
}
